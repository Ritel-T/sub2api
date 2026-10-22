package publicimagehttp

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/stretchr/testify/require"
)

type resolveFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolveFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}
func publicResolver() resolver {
	return resolveFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
}

func TestPublicImageTransportRejectsUnsafeDestinationsBeforeDial(t *testing.T) {
	var calls atomic.Int64
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("must not dial")
	}}
	for _, raw := range []string{"http://127.0.0.1/a", "http://10.0.0.1/", "http://169.254.169.254/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://100.100.100.200/", "http://0.1.2.3/", "http://224.0.0.1/", "http://[64:ff9b::7f00:1]/", "http://[2002:7f00:1::]/", "http://localhost./", "ftp://8.8.8.8/a", "https://user:secret@8.8.8.8/"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
		require.NoError(t, err)
		_, err = Wrap(base).RoundTrip(req)
		require.Error(t, err, raw)
	}
	require.Zero(t, calls.Load())
}

func TestPublicImageTransportRejectsMixedDNSAndEmptyAnswers(t *testing.T) {
	for _, ips := range [][]net.IPAddr{nil, {{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("192.168.1.1")}}} {
		tr := &transport{base: &http.Transport{}, resolver: resolveFunc(func(context.Context, string) ([]net.IPAddr, error) { return ips, nil })}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://images.example/a", nil)
		require.NoError(t, err)
		_, err = tr.RoundTrip(req)
		require.Error(t, err)
	}
}

func TestPublicImageTransportPinsDNSAndPreservesHostAndTLS(t *testing.T) {
	var gotHost, gotSNI, dialAddr string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotHost = r.Host; _, _ = w.Write([]byte("image")) }))
	defer server.Close()
	server.TLS.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) { gotSNI = hello.ServerName; return nil, nil }
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	host := server.Certificate().DNSNames[0]
	var lookups int
	tr := &transport{base: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialAddr = addr
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}, resolver: resolveFunc(func(context.Context, string) ([]net.IPAddr, error) {
		lookups++
		if lookups > 1 {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+host+"/a?sig=kept", nil)
	require.NoError(t, err)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "image", string(body))
	require.Equal(t, "8.8.8.8:443", dialAddr)
	require.Equal(t, host, gotHost)
	require.Equal(t, host, gotSNI)
	require.Equal(t, 1, lookups)
	require.Equal(t, host, req.URL.Host)
}

func TestPublicImageTransportRevalidatesRedirect(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()
	tr := &transport{base: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}, resolver: publicResolver()}
	_, err := (&http.Client{Transport: tr}).Get("http://images.example/a")
	require.ErrorContains(t, err, "not allowed")
	require.Equal(t, 1, calls)
}

func TestPublicImageTransportProxyReceivesPinnedIP(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "images.example", r.Host)
				_, _ = w.Write([]byte("image"))
			}))
			defer target.Close()
			var gotTarget string
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodConnect, r.Method)
				gotTarget = r.Host
				upstream, err := net.Dial("tcp", target.Listener.Addr().String())
				require.NoError(t, err)
				hijacker, ok := w.(http.Hijacker)
				require.True(t, ok)
				client, rw, err := hijacker.Hijack()
				require.NoError(t, err)
				_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				_ = rw.Flush()
				go func() {
					defer func() { _ = upstream.Close() }()
					defer func() { _ = client.Close() }()
					_, _ = io.Copy(upstream, rw)
				}()
				go func() {
					defer func() { _ = upstream.Close() }()
					defer func() { _ = client.Close() }()
					_, _ = io.Copy(client, upstream)
				}()
			})
			var proxy *httptest.Server
			if secure {
				proxy = httptest.NewTLSServer(handler)
			} else {
				proxy = httptest.NewServer(handler)
			}
			defer proxy.Close()
			pu, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			roots := x509.NewCertPool()
			if secure {
				roots.AddCert(proxy.Certificate())
			}
			tr := &transport{base: &http.Transport{Proxy: http.ProxyURL(pu), TLSClientConfig: &tls.Config{RootCAs: roots}}, resolver: publicResolver()}
			resp, err := (&http.Client{Transport: tr}).Get("http://images.example/a")
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, "image", string(body))
			require.Equal(t, "8.8.8.8:80", gotTarget)
		})
	}
}

func TestPublicImageTransportCONNECTUsesIPAndTargetTLSName(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "example.com", r.Host)
		_, _ = w.Write([]byte("image"))
	}))
	defer target.Close()
	var connectHost string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodConnect, r.Method)
		connectHost = r.Host
		upstream, err := net.Dial("tcp", target.Listener.Addr().String())
		require.NoError(t, err)
		hijacker, ok := w.(http.Hijacker)
		require.True(t, ok)
		client, rw, err := hijacker.Hijack()
		require.NoError(t, err)
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		go func() {
			defer func() { _ = upstream.Close() }()
			defer func() { _ = client.Close() }()
			_, _ = io.Copy(upstream, rw)
		}()
		go func() {
			defer func() { _ = upstream.Close() }()
			defer func() { _ = client.Close() }()
			_, _ = io.Copy(client, upstream)
		}()
	}))
	defer proxy.Close()
	pu, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	tr := &transport{base: &http.Transport{Proxy: http.ProxyURL(pu), TLSClientConfig: &tls.Config{RootCAs: roots}}, resolver: publicResolver()}
	resp, err := (&http.Client{Transport: tr}).Get("https://example.com/a")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "8.8.8.8:443", connectHost)
}

func TestPublicImageTransportSOCKSReceivesIP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	target := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		greeting := make([]byte, 3)
		if _, e = io.ReadFull(reader, greeting); e != nil {
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		header := make([]byte, 4)
		if _, e = io.ReadFull(reader, header); e != nil {
			return
		}
		if header[3] != 1 {
			target <- "not IPv4"
			return
		}
		address := make([]byte, 6)
		if _, e = io.ReadFull(reader, address); e != nil {
			return
		}
		target <- net.IP(address[:4]).String()
		_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		req, e := http.ReadRequest(reader)
		if e != nil {
			return
		}
		_ = req.Body.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nimage")
	}()
	pu, err := url.Parse("socks5h://" + listener.Addr().String())
	require.NoError(t, err)
	base := &http.Transport{}
	require.NoError(t, proxyutil.ConfigureTransportProxy(base, pu))
	tr := &transport{base: base, resolver: publicResolver()}
	resp, err := (&http.Client{Transport: tr}).Get("http://images.example/a")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, "8.8.8.8", <-target)
}

func TestPublicImageTransportRejectsUnknownDialPolicy(t *testing.T) {
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://8.8.8.8/", nil)
	require.NoError(t, err)
	_, err = Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })).RoundTrip(req)
	require.ErrorContains(t, err, "cannot enforce")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
