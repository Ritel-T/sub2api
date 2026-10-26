//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const nativeStreamPrivatePayload = "private-body-sk-upstream-secret"

func nativeStreamFixContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *Account, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	account := nativeAnthropicTestAccount()
	account.ID, account.Name, account.Platform = 163, "deepseek-native", PlatformDeepseek
	body := []byte(`{"model":"deepseek-chat","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.AccountID, account.ID))
	return c, rec, account, body
}

func nativeStreamFixService(contentType, payload string) *OpenAIGatewayService {
	cfg := rawChatCompletionsTestConfig()
	// Even explicit error-body logging must never capture a rejected stream body.
	cfg.Gateway.LogUpstreamErrorBody = true
	return &OpenAIGatewayService{
		cfg: cfg,
		httpUpstream: &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{contentType}},
			Body:       io.NopCloser(strings.NewReader(payload)),
		}},
	}
}

func nativeStreamFixEvents(t *testing.T, c *gin.Context) []*OpsUpstreamErrorEvent {
	t.Helper()
	value, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := value.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	return events
}

func TestNativeAnthropicStreamRejectsInvalidResponseBeforeOutput(t *testing.T) {
	html := "<!doctype html><html><body>" + nativeStreamPrivatePayload + "</body></html>"
	tests := []struct{ name, contentType, payload string }{
		{"bare HTML content type", "text/html", html},
		{"HTML charset", "text/html; charset=utf-8", html},
		{"JSON content type", "application/json", `{"error":"` + nativeStreamPrivatePayload + `"}`},
		{"plain text content type", "text/plain", nativeStreamPrivatePayload},
		{"malformed content type", "text/event-stream; charset", html},
		{"missing content type HTML", "", html},
		{"HTML disguised as SSE", "text/event-stream", html},
		{"JSON disguised as SSE", "text/event-stream", `{"error":"` + nativeStreamPrivatePayload + `"}`},
		{"OpenAI SSE", "text/event-stream", "data: {\"choices\":[{\"delta\":{\"content\":\"" + nativeStreamPrivatePayload + "\"}}]}\n\ndata: [DONE]\n\n"},
		{"Responses SSE", "text/event-stream", "event: response.created\ndata: {\"type\":\"response.created\"}\n\n"},
		{"mismatched SSE event", "text/event-stream", "event: response.created\ndata: {\"type\":\"message_start\"}\n\n"},
		{"ping cannot validate HTML", "", "event: ping\ndata: {\"type\":\"ping\"}\n\n" + html},
		{"native error cannot succeed", "text/event-stream", "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"" + nativeStreamPrivatePayload + "\"}}\n\n"},
		{"empty SSE", "text/event-stream", ""},
		{"prelude bounded", "text/event-stream", strings.Repeat(": waiting\n", 7000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec, account, body := nativeStreamFixContext(t)
			svc := nativeStreamFixService(tt.contentType, tt.payload)
			result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, http.StatusBadGateway, failover.StatusCode)
			require.False(t, failover.RetryableOnSameAccount)
			require.False(t, c.Writer.Written(), "the failover handler must still be able to switch accounts")
			require.Empty(t, rec.Body.String())
			require.Empty(t, rec.Header().Get("Content-Type"))
			require.True(t, result == nil || result.Usage.InputTokens == 0)
			events := nativeStreamFixEvents(t, c)
			require.Len(t, events, 1)
			require.Equal(t, account.ID, events[0].AccountID)
			require.Equal(t, PlatformDeepseek, events[0].Platform)
			require.Equal(t, http.StatusOK, events[0].UpstreamStatusCode)
			require.Equal(t, "failover", events[0].Kind)
			require.Empty(t, events[0].Detail)
			require.Empty(t, events[0].UpstreamResponseBody)
			serialized, marshalErr := json.Marshal(events)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(serialized)+err.Error()+string(failover.ResponseBody), nativeStreamPrivatePayload)
			_, marked := GetOpsStreamError(c)
			require.False(t, marked, "a recoverable attempt must not mark the final request failed")
		})
	}
}

func TestNativeAnthropicStreamValidSSEPreservesBytesAndUsage(t *testing.T) {
	valid := nativeAnthropicStreamResponse()
	payloadBytes, err := io.ReadAll(valid.Body)
	require.NoError(t, err)
	prefix := ": native prelude\n\nevent: ping\ndata: {\"type\":\"ping\"}\n\n"
	for _, contentType := range []string{"text/event-stream", "text/event-stream; charset=utf-8", ""} {
		t.Run("content type="+contentType, func(t *testing.T) {
			c, rec, account, body := nativeStreamFixContext(t)
			payload := prefix + string(payloadBytes)
			svc := nativeStreamFixService(contentType, payload)
			result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, payload, rec.Body.String())
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
			require.Equal(t, 93, result.Usage.InputTokens)
			require.Equal(t, 16, result.Usage.OutputTokens)
			_, marked := GetOpsStreamError(c)
			require.False(t, marked)
			_, hasEvents := c.Get(OpsUpstreamErrorsKey)
			require.False(t, hasEvents)
		})
	}
}

func TestNativeAnthropicStreamMissingTerminalMarksCurrentAccountAndPreservesUsage(t *testing.T) {
	for _, suffix := range []string{"", "event: message_stop\n", "data: [DONE]\n\n"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			c, rec, account, body := nativeStreamFixContext(t)
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				AccountID: 122, Platform: PlatformDeepseek, UpstreamStatusCode: http.StatusTooManyRequests,
				Kind: "failover", Message: "previous attempt rate limited",
			})
			setOpsUpstreamError(c, http.StatusTooManyRequests, "previous attempt rate limited", "previous attempt detail")
			payload := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"deepseek-chat\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":3,\"cache_creation_input_tokens\":2}}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n"
			payload += suffix
			svc := nativeStreamFixService("text/event-stream", payload)
			result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.ErrorContains(t, err, "missing terminal event")
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "already written streams must not be replayed")
			require.Equal(t, payload, rec.Body.String())
			require.NotContains(t, rec.Body.String(), `"type":"message_stop"`)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, 15, result.Usage.InputTokens)
			require.Equal(t, 7, result.Usage.OutputTokens)
			require.Equal(t, 3, result.Usage.CacheReadInputTokens)
			require.Equal(t, 2, result.Usage.CacheCreationInputTokens)
			events := nativeStreamFixEvents(t, c)
			require.Len(t, events, 2)
			require.Equal(t, int64(122), events[0].AccountID)
			require.Equal(t, http.StatusTooManyRequests, events[0].UpstreamStatusCode)
			require.Equal(t, account.ID, events[1].AccountID)
			require.Equal(t, http.StatusOK, events[1].UpstreamStatusCode)
			require.Equal(t, "stream_failed", events[1].Kind)
			require.Equal(t, "stream_incomplete", events[1].Reason)
			mark, ok := GetOpsStreamError(c)
			require.True(t, ok)
			require.True(t, mark.CountTowardsSLA)
			require.Equal(t, "upstream_error", mark.ErrType)
			require.Equal(t, "stream_incomplete", mark.Code)
			require.Equal(t, http.StatusBadGateway, mark.IntendedStatus)
			require.Equal(t, http.StatusOK, mark.UpstreamStatus)
			require.Equal(t, account.ID, mark.AccountID)
			require.Equal(t, "deepseek-chat", mark.UpstreamModel)
			require.Len(t, mark.UpstreamErrors, 2)
			require.Equal(t, account.ID, mark.UpstreamErrors[1].AccountID)
			require.Empty(t, mark.UpstreamDetail, "previous attempt details must not leak into the current failure")
		})
	}
}

func TestNativeAnthropicStreamInvalidResponseAfterClientCommitCannotFailover(t *testing.T) {
	c, rec, account, body := nativeStreamFixContext(t)
	_, err := io.WriteString(c.Writer, ": existing client keepalive\n\n")
	require.NoError(t, err)
	svc := nativeStreamFixService("text/html", nativeStreamPrivatePayload)
	_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	require.Equal(t, ": existing client keepalive\n\n", rec.Body.String())
	mark, ok := GetOpsStreamError(c)
	require.True(t, ok)
	require.True(t, mark.CountTowardsSLA)
	require.Equal(t, account.ID, mark.AccountID)
	require.Equal(t, http.StatusOK, mark.UpstreamStatus)
	require.Equal(t, http.StatusBadGateway, mark.IntendedStatus)
}

func TestNativeAnthropicStreamFailureCanRecoverOnNextAccount(t *testing.T) {
	c, rec, account, body := nativeStreamFixContext(t)
	svc := nativeStreamFixService("text/html", nativeStreamPrivatePayload)
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.True(t, failover.ShouldRetryNextAccount())
	require.False(t, failover.IsCredentialFailure())
	nextAccount := nativeAnthropicTestAccount()
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.AccountID, nextAccount.ID))
	svc.httpUpstream = &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
	result, err := svc.ForwardAsAnthropic(context.Background(), c, nextAccount, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, rec.Body.String(), "pong")
	require.NotContains(t, rec.Body.String(), nativeStreamPrivatePayload)
	_, marked := GetOpsStreamError(c)
	require.False(t, marked)
	require.Len(t, nativeStreamFixEvents(t, c), 1)
}

func TestNativeAnthropicStreamErrorAfterOutputCannotBecomeSuccess(t *testing.T) {
	c, rec, account, body := nativeStreamFixContext(t)
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n"
	payload := start + "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"" + nativeStreamPrivatePayload + "\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	svc := nativeStreamFixService("text/event-stream", payload)
	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.Error(t, err)
	require.Equal(t, 10, result.Usage.InputTokens)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	require.NotContains(t, rec.Body.String(), nativeStreamPrivatePayload)
	require.NotContains(t, rec.Body.String(), "message_stop")
	mark, ok := GetOpsStreamError(c)
	require.True(t, ok)
	require.Equal(t, account.ID, mark.AccountID)
	require.Equal(t, "stream_failed", mark.Code)
}

func TestNativeAnthropicStreamReadFailurePreservesOutcomeAndUsage(t *testing.T) {
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n"
	stop := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, tt := range []struct {
		name, payload          string
		disconnected, complete bool
		readErr                error
	}{
		{name: "before output"},
		{name: "partial usage", payload: start},
		{name: "disconnected partial usage", payload: start, disconnected: true},
		{name: "cancel before output", readErr: context.Canceled},
		{name: "cancel after partial usage", payload: start, readErr: context.Canceled},
		{name: "deadline after partial usage", payload: start, readErr: context.DeadlineExceeded},
		{name: "read error after valid terminal", payload: start + stop, complete: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _, account, body := nativeStreamFixContext(t)
			if tt.disconnected {
				c.Writer = &openAIRawStreamDisconnectedWriter{ResponseWriter: c.Writer}
			}
			readErr := tt.readErr
			if readErr == nil {
				readErr = errors.New(nativeStreamPrivatePayload)
			}
			svc := nativeStreamFixService("text/event-stream", "")
			svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: &errTailReader{data: []byte(tt.payload), err: readErr},
			}}
			result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			if tt.complete {
				require.NoError(t, err)
				_, marked := GetOpsStreamError(c)
				require.False(t, marked)
				return
			}
			require.Error(t, err)
			require.NotContains(t, err.Error(), nativeStreamPrivatePayload)
			var failover *UpstreamFailoverError
			if tt.disconnected || tt.readErr != nil {
				require.False(t, errors.As(err, &failover))
				require.Equal(t, tt.disconnected, result.ClientDisconnect)
				if tt.payload != "" {
					require.Equal(t, 10, result.Usage.InputTokens)
				}
				if tt.readErr != nil {
					require.ErrorIs(t, err, tt.readErr)
				}
				_, marked := GetOpsStreamError(c)
				require.False(t, marked, "client departure/cancellation must retain its original attribution")
				_, hasEvents := c.Get(OpsUpstreamErrorsKey)
				require.False(t, hasEvents, "known cancellation must not create provider failure events")
				return
			}
			if tt.payload == "" {
				require.ErrorAs(t, err, &failover)
				require.False(t, c.Writer.Written())
			} else {
				require.False(t, errors.As(err, &failover))
				require.Equal(t, 10, result.Usage.InputTokens)
				mark, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Equal(t, account.ID, mark.AccountID)
				require.Equal(t, "stream_read_error", mark.Code)
			}
			eventJSON, marshalErr := json.Marshal(nativeStreamFixEvents(t, c))
			require.NoError(t, marshalErr)
			require.NotContains(t, string(eventJSON), nativeStreamPrivatePayload)
		})
	}
}

// The upstream may finish its read as EOF after the downstream request was
// canceled. Do not classify that known cancellation as a provider truncation.
type nativeStreamCancelAtEOFReader struct {
	io.Reader
	cancel context.CancelFunc
}

func (r *nativeStreamCancelAtEOFReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.cancel()
	}
	return n, err
}

func TestNativeAnthropicStreamCanceledEOFDoesNotCreateProviderFailure(t *testing.T) {
	c, _, account, body := nativeStreamFixContext(t)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	payload := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n"
	svc := nativeStreamFixService("text/event-stream", "")
	svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(&nativeStreamCancelAtEOFReader{Reader: strings.NewReader(payload), cancel: cancel}),
	}}
	result, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 10, result.Usage.InputTokens)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	_, marked := GetOpsStreamError(c)
	require.False(t, marked)
	_, hasEvents := c.Get(OpsUpstreamErrorsKey)
	require.False(t, hasEvents)
}

func TestNativeAnthropicStreamKeepaliveDoesNotCommitUnvalidatedResponse(t *testing.T) {
	c, rec, account, body := nativeStreamFixContext(t)
	svc := nativeStreamFixService("", "")
	svc.cfg.Gateway.StreamKeepaliveInterval = 1
	svc.cfg.Gateway.StreamDataIntervalTimeout = 3
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: reader,
	}}
	go func() {
		time.Sleep(1200 * time.Millisecond)
		_, _ = io.WriteString(writer, "<html>"+nativeStreamPrivatePayload+"</html>\n")
		_ = writer.Close()
	}()
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestNativeAnthropicStreamUnvalidatedTimeoutCanFailover(t *testing.T) {
	c, rec, account, body := nativeStreamFixContext(t)
	svc := nativeStreamFixService("", "")
	svc.cfg.Gateway.StreamKeepaliveInterval = 1
	svc.cfg.Gateway.StreamDataIntervalTimeout = 1
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	svc.httpUpstream = &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: reader,
	}}
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
	require.Equal(t, "stream_timeout", nativeStreamFixEvents(t, c)[0].Reason)
}
