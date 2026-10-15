package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// Rewrite only the test dial target. The real WS client, pool, request payload,
// event parser, SSE writer and Forward outcome all run against loopback.
type wsSSETimingLoopbackDialer struct {
	target string
	inner  openAIWSClientDialer
}

func (d *wsSSETimingLoopbackDialer) Dial(ctx context.Context, _ string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	return d.inner.Dial(ctx, d.target, headers, proxy)
}

type wsSSETimingWriter struct {
	gin.ResponseWriter
	collector  *requesttiming.Collector
	failWrites bool
}

func (w *wsSSETimingWriter) Write(p []byte) (int, error) {
	start := time.Now()
	if w.failWrites {
		err := errors.New("test downstream closed")
		w.collector.Written(start, 0, err, false)
		return 0, err
	}
	n, err := w.ResponseWriter.Write(p)
	w.collector.Written(start, n, err, false)
	return n, err
}

func (w *wsSSETimingWriter) Flush() {
	start := time.Now()
	w.ResponseWriter.Flush()
	w.collector.Written(start, 0, nil, true)
}

func wsSSETimingLoopback(t *testing.T, events []string, accelerate, failWrites bool) (*OpenAIForwardResult, error, requesttiming.Snapshot, string) {
	t.Helper()
	var submissions atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("loopback upgrade: %v", err)
			return
		}
		defer conn.Close()
		var payload map[string]any
		if err = conn.ReadJSON(&payload); err != nil {
			t.Errorf("loopback payload: %v", err)
			return
		}
		submissions.Add(1)
		if payload["type"] != "response.create" {
			t.Errorf("unexpected request event: %v", payload["type"])
			return
		}
		for _, event := range events {
			if err = conn.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	cfg := newOpenAIWSV2TestConfig()
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 2
	cfg.Gateway.OpenAIWS.EventFlushBatchSize = 100
	cfg.Gateway.OpenAIWS.EventFlushIntervalMS = 60000
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&wsSSETimingLoopbackDialer{target: "ws" + strings.TrimPrefix(server.URL, "http"), inner: newDefaultOpenAIWSClientDialer()})
	defer pool.Close()
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	if !accelerate {
		SetOpenAIClientTransport(c, OpenAIClientTransportWS)
	}
	collector := requesttiming.New(time.Now(), -1)
	c.Writer = &wsSSETimingWriter{ResponseWriter: c.Writer, collector: collector, failWrites: failWrites}
	ctx := requesttiming.With(context.Background(), collector)
	result, err := svc.Forward(ctx, c, wsSSETestAccount(), []byte(wsSSETestRequest))
	collector.Finish(c.Writer.Status(), ctx.Err() != nil)
	var data requesttiming.Snapshot
	collector.WhenFinished(func(v requesttiming.Snapshot) { data = v })
	require.EqualValues(t, 1, submissions.Load(), "observations must never add/replay a response.create")
	require.Nil(t, upstream.lastReq, "a sent WS turn must not fall back to HTTP")
	return result, err, data, rec.Body.String()
}

func TestOpenAIWSSSEAccelerationRequestTimingLoopback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const created = `{"type":"response.created","response":{"id":"resp_timing"}}`
	const text = `{"type":"response.output_text.delta","delta":"hello"}`
	const complete = `{"type":"response.completed","response":{"id":"resp_timing","usage":{"input_tokens":3,"output_tokens":1}}}`
	cases := []struct {
		name               string
		events             []string
		terminal, outcome  string
		visible, wantError bool
	}{
		{"completed", []string{created, text, complete}, "completed", "success", true, false},
		{"done", []string{created, text, `{"type":"response.done","response":{"id":"resp_timing","usage":{"input_tokens":3,"output_tokens":1}}}`}, "completed", "success", true, false},
		{"reasoning_structure", []string{created, `{"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"synthetic-private"}}`, complete}, "completed", "success", false, false},
		{"failed", []string{created, `{"type":"response.failed","response":{"id":"resp_timing","error":{"code":"invalid_request_error","message":"synthetic"}}}`}, "failed", "failed", false, false},
		{"incomplete", []string{created, text, `{"type":"response.incomplete","response":{"id":"resp_timing","incomplete_details":{"reason":"max_output_tokens"}}}`}, "incomplete", "failed", true, false},
		{"cancelled", []string{created, `{"type":"response.cancelled","response":{"id":"resp_timing"}}`}, "canceled", "canceled", false, false},
		{"canceled_alias", []string{created, `{"type":"response.canceled","response":{"id":"resp_timing"}}`}, "canceled", "canceled", false, false},
		{"error", []string{created, `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_request_error","message":"synthetic"}}`}, "failed", "failed", false, true},
		{"disconnect_after_output", []string{created, text}, "", "failed", true, true},
		{"metadata_disconnect", []string{created, `{"type":"response.in_progress","response":{"id":"resp_timing"}}`}, "", "failed", false, true},
		{"tool_arguments", []string{created, `{"type":"response.function_call_arguments.delta","delta":"{\"x\":1}"}`, complete}, "completed", "success", true, false},
		{"tool_item_payload", []string{created, `{"type":"response.output_item.added","item":{"type":"function_call","arguments":"{}"}}`, complete}, "completed", "success", true, false},
		{"empty_delta", []string{created, `{"type":"response.output_text.delta","delta":""}`, complete}, "completed", "success", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err, data, body := wsSSETimingLoopback(t, tc.events, true, false)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}
			require.Equal(t, tc.terminal, data.Terminal)
			require.Equal(t, tc.outcome, data.Outcome)
			require.Contains(t, data.Events, openAIOAuthWSSSEAccelerationReason)
			require.Equal(t, OpenAITTFTModeSemantic, data.TTFTMode)
			require.Empty(t, data.Attempts, "do not fabricate HTTP transport attempts for WS")
			require.Contains(t, data.Events, "first_flush", "the metadata frame should actually flush")
			_, visible := data.Events["first_visible"]
			require.Equal(t, tc.visible, visible)
			if tc.visible {
				require.Less(t, data.Events["first_flush"], data.Events["first_visible"], "metadata flush is not visible output")
				require.Contains(t, data.Events, "first_output_flush")
				require.GreaterOrEqual(t, data.Events["first_output_flush"], data.Events["first_visible"])
			}
			if tc.name == "reasoning_structure" {
				require.Contains(t, data.Events, "first_semantic")
				require.NotContains(t, data.Events, "first_visible")
			}
			if tc.name == "metadata_disconnect" {
				require.NotContains(t, data.Events, "first_semantic")
				require.NotContains(t, data.Events, "first_output_flush")
			}
			if tc.terminal == "" {
				require.NotContains(t, body, "response.completed")
				require.NotContains(t, data.Events, "terminal")
			}
		})
	}
}

func TestOpenAIWSSSEAccelerationTimingDoesNotAffectNativeWS(t *testing.T) {
	result, err, data, _ := wsSSETimingLoopback(t, []string{
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.completed","response":{"id":"resp_native","usage":{"input_tokens":1,"output_tokens":1}}}`,
	}, false, false)
	require.NoError(t, err)
	require.True(t, result.OpenAIWSMode)
	require.NotNil(t, result.FirstTokenMs)
	require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
	require.NotContains(t, data.Events, openAIOAuthWSSSEAccelerationReason)
	require.NotContains(t, data.Events, "first_visible")
	require.NotContains(t, data.Events, "first_output_flush")
	require.Empty(t, data.Terminal)
	require.Empty(t, data.TTFTMode)
}

func TestOpenAIWSSSEAccelerationTimingDoesNotClaimFailedFlush(t *testing.T) {
	result, err, data, body := wsSSETimingLoopback(t, []string{
		`{"type":"response.created","response":{"id":"resp_closed"}}`,
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.completed","response":{"id":"resp_closed","usage":{"input_tokens":1,"output_tokens":1}}}`,
	}, true, true)
	require.NoError(t, err, "bounded drain retains the observed completed usage")
	require.True(t, result.ClientDisconnect)
	require.True(t, data.DownstreamError)
	require.Equal(t, "client_disconnected", data.Outcome)
	require.Equal(t, "completed", data.Terminal, "observed upstream terminal is not client receipt")
	require.Empty(t, body)
	require.NotContains(t, data.Events, "first_output_flush")
	require.NotContains(t, data.Events, "first_flush")
}

// A safe handshake rejection can enter the opt-in branch and then succeed over
// HTTP. The marker means entered, while the final result and reason stay HTTP.
func TestOpenAIWSSSEAccelerationTimingKeepsHandshakeFallbackHTTP(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	dialer := &wsSSETestDialer{status: http.StatusUpgradeRequired, err: errors.New("synthetic handshake rejection")}
	events := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fallback\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(events))}}
	svc := wsSSETestService(t, dialer, upstream)
	collector := requesttiming.New(time.Now(), -1)
	c.Writer = &wsSSETimingWriter{ResponseWriter: c.Writer, collector: collector}
	ctx := requesttiming.With(context.Background(), collector)
	result, err := svc.Forward(ctx, c, wsSSETestAccount(), []byte(wsSSETestRequest))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.OpenAIWSMode)
	require.NotNil(t, upstream.lastReq)
	require.EqualValues(t, 1, dialer.calls.Load())
	require.Equal(t, "oauth_ws_sse_handshake_fallback", c.GetString("openai_ws_transport_reason"))
	collector.Finish(c.Writer.Status(), false)
	collector.WhenFinished(func(data requesttiming.Snapshot) {
		require.Equal(t, "success", data.Outcome)
		require.Equal(t, "completed", data.Terminal)
		require.Contains(t, data.Events, openAIOAuthWSSSEAccelerationReason)
		require.Contains(t, data.Events, "first_visible")
		require.Contains(t, data.Events, "first_output_flush")
	})
}
