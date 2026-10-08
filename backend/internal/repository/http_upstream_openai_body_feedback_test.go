package repository

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func openAIBodyFeedbackProxy(t *testing.T, target string) *httptest.Server {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		upstream, err := net.Dial("tcp", strings.TrimPrefix(target, "https://"))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		downstream, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		if _, err = buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n"); err == nil {
			err = buffered.Flush()
		}
		if err != nil {
			_ = upstream.Close()
			_ = downstream.Close()
			return
		}
		go func() { _, _ = io.Copy(upstream, buffered); _ = upstream.Close(); _ = downstream.Close() }()
		go func() { _, _ = io.Copy(downstream, upstream); _ = downstream.Close(); _ = upstream.Close() }()
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

func TestOpenAIHTTP2BodyFeedbackCompletionAndBoundaries(t *testing.T) {
	proxy := "http://127.0.0.1:18080"
	protocolFailure := http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}
	for _, tc := range []struct {
		name        string
		profile     service.HTTPUpstreamProfile
		mode, proxy string
		proto       int
		body        func() io.ReadCloser
		cancel      bool
		closeOnly   bool
		disable     bool
		wantCount   int
		wantActive  bool
	}{
		{"normal EOF resets after completion", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return io.NopCloser(strings.NewReader("done")) }, false, false, false, 0, false},
		{"headers and early close do not reset", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return io.NopCloser(strings.NewReader("unread")) }, false, true, false, 1, false},
		{"stream error counted once", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, false, 0, true},
		{"ordinary unexpected EOF not proven H2 failure", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{io.ErrUnexpectedEOF} }, false, false, false, 1, false},
		{"cancelled caller", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, true, false, false, 1, false},
		{"cancelled read", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{fmt.Errorf("stream error: %w", context.Canceled)} }, false, false, false, 1, false},
		{"timeout read", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{fmt.Errorf("stream error: %w", context.DeadlineExceeded)} }, false, false, false, 1, false},
		{"actual H1 is outside H2 feedback", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 1, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, false, 1, false},
		{"fallback H1 does not change H2 window", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH1Fallback, proxy, 1, func() io.ReadCloser { return io.NopCloser(strings.NewReader("done")) }, false, false, false, 1, false},
		{"long stream profile remains isolated", service.HTTPUpstreamProfileLongStream, upstreamProtocolModeLongStreamH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, false, 1, false},
		{"fingerprint pool remains outside existing mode", service.HTTPUpstreamProfileOpenAI, "", proxy, 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, false, 1, false},
		{"direct remains outside proxy fallback", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, directProxyKey, 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, false, 1, false},
		{"SOCKS remains outside proxy fallback", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, "socks5://127.0.0.1:18080", 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, false, 1, false},
		{"fallback disabled", service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxy, 2, func() io.ReadCloser { return bpsErrorBody{protocolFailure} }, false, false, true, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Gateway: config.GatewayConfig{OpenAIHTTP2: config.GatewayOpenAIHTTP2Config{Enabled: true, AllowProxyFallbackToHTTP1: !tc.disable, FallbackErrorThreshold: 2}}}
			svc, ok := NewHTTPUpstream(cfg).(*httpUpstreamService)
			require.True(t, ok)
			state := svc.getOrCreateOpenAIHTTP2FallbackState(tc.proxy)
			state.recordFailure(time.Now(), 2, time.Minute, 10*time.Minute)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
			require.NoError(t, err)
			resp := &http.Response{ProtoMajor: tc.proto, Body: tc.body()}
			svc.wrapOpenAIHTTP2Feedback(req, resp, tc.profile, &upstreamClientEntry{protocolMode: tc.mode, proxyKey: tc.proxy})
			state.mu.Lock()
			headerCount := state.errorCount
			state.mu.Unlock()
			require.Equal(t, 1, headerCount, "headers never prove a complete transport response")
			if tc.cancel {
				cancel()
			}
			if !tc.closeOnly {
				_, _ = io.ReadAll(resp.Body)
				_, _ = resp.Body.Read(make([]byte, 1))
			}
			require.NoError(t, resp.Body.Close())
			state.mu.Lock()
			count := state.errorCount
			state.mu.Unlock()
			require.Equal(t, tc.wantCount, count)
			require.Equal(t, tc.wantActive, svc.isOpenAIHTTP2FallbackActive(tc.proxy))
			require.False(t, svc.isOpenAIHTTP2FallbackActive("http://127.0.0.1:18081"), "other proxy state is untouched")
		})
	}
}

func TestOpenAIHTTP2CompatibilityClassificationIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"typed connection protocol", http2.ConnectionError(http2.ErrCodeProtocol), true},
		{"typed stream protocol", http2.StreamError{Code: http2.ErrCodeProtocol}, true},
		{"typed stream cancel", http2.StreamError{Code: http2.ErrCodeCancel}, false},
		{"typed GOAWAY", http2.GoAwayError{ErrCode: http2.ErrCodeProtocol}, true},
		{"wrapped internal error", fmt.Errorf("body read: %w", http2.StreamError{Code: http2.ErrCodeInternal}), true},
		{"Go net/http string stream error", errors.New("stream error: stream ID 5; INTERNAL_ERROR; received from peer"), true},
		{"Go net/http cancelled stream", errors.New("stream error: stream ID 5; CANCEL; received from peer"), false},
		{"ordinary EOF", io.EOF, false},
		{"incomplete generic response", io.ErrUnexpectedEOF, false},
		{"cancelled", context.Canceled, false},
		{"ordinary timeout", errors.New("http2: i/o timeout"), false},
		{"application failure", errors.New("upstream rejected model"), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, isOpenAIHTTP2CompatibilityError(tc.err)) })
	}
}

func TestOpenAIHTTP2BodyErrorFallsBackOnlyOnNextIndependentRequest(t *testing.T) {
	for _, path := range []string{"ordinary", "tls_nil", "regional"} {
		t.Run(path, func(t *testing.T) {
			var h2Calls, h1Calls atomic.Int32
			release := make(chan struct{}, 2)
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.ProtoMajor != 2 {
					h1Calls.Add(1)
					_, _ = io.WriteString(w, "ok")
					return
				}
				h2Calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: started\n\n")
				_ = http.NewResponseController(w).Flush()
				select {
				case <-release:
					panic(http.ErrAbortHandler) // Real RST_STREAM INTERNAL_ERROR after headers.
				case <-r.Context().Done():
				}
			}))
			target.EnableHTTP2 = true
			target.StartTLS()
			t.Cleanup(target.Close)
			proxy := openAIBodyFeedbackProxy(t, target.URL)
			cfg := &config.Config{}
			if path == "regional" {
				cfg = regionalHTTPConfig(t, proxy.URL, "127.0.0.1")
			}
			cfg.Gateway.OpenAIHTTP2 = config.GatewayOpenAIHTTP2Config{Enabled: true, AllowProxyFallbackToHTTP1: true, FallbackErrorThreshold: 2, FallbackWindowSeconds: 60, FallbackTTLSeconds: 600}
			svc, ok := NewHTTPUpstream(cfg).(*httpUpstreamService)
			require.True(t, ok)
			profile := service.HTTPUpstreamProfileOpenAI
			trust := func() *upstreamClientEntry {
				entry, err := svc.getClientEntry(proxy.URL, 300, 4, profile, false, false)
				require.NoError(t, err)
				transport, ok := entry.client.Transport.(*http.Transport)
				require.True(t, ok)
				roots := x509.NewCertPool()
				roots.AddCert(target.Certificate())
				transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
				t.Cleanup(transport.CloseIdleConnections)
				return entry
			}
			trust()
			request := func(payload string) (*http.Response, error) {
				ctx := service.WithHTTPUpstreamProfile(t.Context(), profile)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, strings.NewReader(payload))
				require.NoError(t, err)
				if path == "tls_nil" {
					return svc.DoWithTLS(req, proxy.URL, 300, 4, nil)
				}
				proxyURL := proxy.URL
				if path == "regional" {
					proxyURL = ""
				}
				return svc.Do(req, proxyURL, 300, 4)
			}
			for i := range 2 {
				resp, err := request("synthetic independent request")
				require.NoError(t, err)
				require.Equal(t, 2, resp.ProtoMajor)
				if i == 1 {
					state := svc.getOrCreateOpenAIHTTP2FallbackState(proxy.URL)
					state.mu.Lock()
					count := state.errorCount
					state.mu.Unlock()
					require.Equal(t, 1, count, "headers cannot clear the previous body failure")
				}
				release <- struct{}{}
				_, err = io.ReadAll(resp.Body)
				require.ErrorContains(t, err, "INTERNAL_ERROR")
				require.NoError(t, resp.Body.Close())
				require.EqualValues(t, i+1, h2Calls.Load(), "a sent POST is never replayed")
				require.Zero(t, h1Calls.Load())
				require.Equal(t, i == 1, svc.isOpenAIHTTP2FallbackActive(proxy.URL))
			}
			require.Equal(t, upstreamProtocolModeOpenAIH1Fallback, trust().protocolMode)
			resp, err := request("new independent request")
			require.NoError(t, err)
			require.Equal(t, 1, resp.ProtoMajor)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, "ok", string(body))
			require.NoError(t, resp.Body.Close())
			require.EqualValues(t, 2, h2Calls.Load())
			require.EqualValues(t, 1, h1Calls.Load())
			requireNoUpstreamInFlight(t, svc)
		})
	}
}
