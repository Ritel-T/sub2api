//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type deepSeekRecoveryUpstreamStub struct {
	do    func(*http.Request) (*http.Response, error)
	calls atomic.Int32
}

func (u *deepSeekRecoveryUpstreamStub) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls.Add(1)
	return u.do(r)
}
func (u *deepSeekRecoveryUpstreamStub) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(r, p, id, n)
}

func deepSeekRecoveryRequest(t *testing.T, ctx context.Context, path string) *http.Request {
	t.Helper()
	detached, release := detachAPIKeyUpstreamContext(ctx)
	defer release()
	r, err := http.NewRequestWithContext(detached, http.MethodPost, "https://upstream.invalid"+path, strings.NewReader(`{"model":"deepseek-flash","stream":true}`))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func deepSeekRecoveryFixture(t *testing.T, path, contentType, raw string) (*http.Response, *DeepSeekRecoveryAttempt) {
	t.Helper()
	ctx, attempt := NewDeepSeekRecoveryAttempt(context.Background())
	t.Cleanup(attempt.Close)
	u := &deepSeekRecoveryUpstreamStub{do: func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	}}
	s := &OpenAIGatewayService{httpUpstream: u}
	resp, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, ctx, path), "", &Account{ID: 7, Platform: "deepseek"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, attempt
}

func deepSeekRecoverySSE(payloads ...string) string {
	var s strings.Builder
	for _, p := range payloads {
		s.WriteString("data: " + p + "\n\n")
	}
	return s.String()
}

func TestDeepSeekRecoverySourceJSON(t *testing.T) {
	tests := []struct {
		name, path, raw     string
		fail, tool, refusal bool
	}{
		{"cc_text", "/v1/chat/completions", `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}]}`, false, false, false},
		{"cc_refusal", "/v1/chat/completions", `{"choices":[{"message":{"content":null,"refusal":"I cannot help"},"finish_reason":"stop"}]}`, false, false, true},
		{"cc_empty_filter", "/v1/chat/completions", `{"choices":[{"message":{"content":""},"finish_reason":"content_filter"}]}`, false, false, true},
		{"cc_refusal_block", "/v1/chat/completions", `{"choices":[{"message":{"content":[{"type":"refusal","refusal":"no"}]},"finish_reason":"stop"}]}`, false, false, true},
		{"cc_empty_stop", "/v1/chat/completions", `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`, true, false, false},
		{"cc_reasoning_only", "/v1/chat/completions", `{"choices":[{"message":{"reasoning_content":"thinking"},"finish_reason":"stop"}]}`, true, false, false},
		{"cc_length_answer", "/v1/chat/completions", `{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`, false, false, false},
		{"cc_length_no_answer", "/v1/chat/completions", `{"choices":[{"message":{"reasoning_content":"thinking"},"finish_reason":"length"}]}`, true, false, false},
		{"cc_aborted", "/v1/chat/completions", `{"choices":[{"message":{"content":"partial"},"finish_reason":"aborted"}]}`, true, false, false},
		{"cc_resources", "/v1/chat/completions", `{"choices":[{"message":{"content":"partial"},"finish_reason":"insufficient_system_resource"}]}`, true, false, false},
		{"cc_error", "/v1/chat/completions", `{"error":{"message":"upstream failure"}}`, true, false, false},
		{"cc_html", "/v1/chat/completions", `<html>error</html>`, true, false, false},
		{"cc_bad_json", "/v1/chat/completions", `{"choices":`, true, false, false},
		{"cc_missing_finish", "/v1/chat/completions", `{"choices":[{"message":{"content":"partial"}}]}`, true, false, false},
		{"cc_usage_only", "/v1/chat/completions", `{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":4}}`, true, false, false},
		{"cc_tool", "/v1/chat/completions", `{"choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, false, true, false},
		{"cc_bad_tool", "/v1/chat/completions", `{"choices":[{"message":{"tool_calls":[{"function":{"name":"lookup","arguments":"{"}}]},"finish_reason":"tool_calls"}]}`, true, false, false},
		{"anthropic_text", "/v1/messages", `{"type":"message","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`, false, false, false},
		{"anthropic_tool", "/v1/messages", `{"type":"message","content":[{"type":"tool_use","id":"tool1","name":"lookup","input":{}}],"stop_reason":"tool_use"}`, false, true, false},
		{"anthropic_thinking", "/v1/messages", `{"type":"message","content":[{"type":"thinking","thinking":"thoughts"}],"stop_reason":"end_turn"}`, true, false, false},
		{"anthropic_length", "/v1/messages", `{"type":"message","content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens"}`, false, false, false},
		{"anthropic_refusal", "/v1/messages", `{"type":"message","content":[],"stop_reason":"refusal"}`, false, false, true},
		{"anthropic_error", "/v1/messages", `{"type":"error","error":{"type":"overloaded_error"}}`, true, false, false},
		{"responses_text", "/v1/responses", `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`, false, false, false},
		{"responses_refusal", "/v1/responses", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"no"}]}]}`, false, false, true},
		{"responses_tool", "/v1/responses", `{"status":"completed","output":[{"type":"function_call","name":"lookup","arguments":"{}"}]}`, false, true, false},
		{"responses_thinking", "/v1/responses", `{"status":"completed","output":[{"type":"reasoning","summary":[{"text":"thinking"}]}]}`, true, false, false},
		{"responses_empty", "/v1/responses", `{"status":"completed","output":[]}`, true, false, false},
		{"responses_failed", "/v1/responses", `{"status":"failed","output":[]}`, true, false, false},
		{"responses_length", "/v1/responses", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`, false, false, false},
		{"responses_filter", "/v1/responses", `{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, a := deepSeekRecoveryFixture(t, tt.path, "application/json", tt.raw)
			got, err := io.ReadAll(resp.Body)
			if tt.fail {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Error(t, a.Err(), "converter may swallow the read error; structured state must retain it")
				require.False(t, failover.RetryableOnSameAccount)
				require.True(t, failover.ShouldRetryNextAccount())
				require.Empty(t, got, "invalid JSON results must never reach a converter")
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.raw, string(got))
				require.NoError(t, resp.Body.Close())
				require.NoError(t, a.Err())
				require.True(t, a.Outcome().Terminal)
				require.Equal(t, tt.tool, a.Outcome().ToolCall)
				require.Equal(t, tt.refusal, a.Outcome().Refusal)
			}
		})
	}
}

func TestDeepSeekRecoverySourceSSE(t *testing.T) {
	ccText := `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`
	ccStop := `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	ccThinking := `{"choices":[{"delta":{"reasoning_content":"thought"},"finish_reason":null}]}`
	anthText := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`
	anthStop := `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`
	anthEnd := `{"type":"message_stop"}`
	rspText := `{"type":"response.output_text.delta","output_index":0,"delta":"hello"}`
	rspEnd := `{"type":"response.completed","response":{"status":"completed"}}`
	tests := []struct {
		name, path, raw, forbidden string
		fail                       bool
	}{
		{"cc_complete", "/chat/completions", deepSeekRecoverySSE(ccText, ccStop, "[DONE]"), "", false},
		{"cc_terminal_without_done", "/chat/completions", deepSeekRecoverySSE(ccText, ccStop), "", false},
		{"cc_done_only", "/chat/completions", deepSeekRecoverySSE("[DONE]"), "[DONE]", true},
		{"cc_usage_done", "/chat/completions", deepSeekRecoverySSE(`{"choices":[],"usage":{"completion_tokens":2}}`, "[DONE]"), "[DONE]", true},
		{"cc_text_then_done", "/chat/completions", deepSeekRecoverySSE(ccText, "[DONE]"), "[DONE]", true},
		{"cc_text_then_eof", "/chat/completions", deepSeekRecoverySSE(ccText), "", true},
		{"cc_thinking_then_stop", "/chat/completions", deepSeekRecoverySSE(ccThinking, ccStop, "[DONE]"), `"finish_reason":"stop"`, true},
		{"cc_failure_finish", "/chat/completions", deepSeekRecoverySSE(ccText, `{"choices":[{"delta":{},"finish_reason":"insufficient_system_resource"}]}`, "[DONE]"), "insufficient_system_resource", true},
		{"cc_bad_frame", "/chat/completions", deepSeekRecoverySSE(ccText, `{bad json}`), "bad json", true},
		{"cc_html", "/chat/completions", "<html>oops</html>\n\n", "html", true},
		{"cc_error_envelope", "/chat/completions", deepSeekRecoverySSE(ccText, `{"error":{"message":"quota"}}`), "quota", true},
		{"cc_partial_frame", "/chat/completions", deepSeekRecoverySSE(ccText) + "data: " + ccStop + "\n", `"finish_reason":"stop"`, true},
		{"anthropic_complete", "/messages", deepSeekRecoverySSE(anthText, anthStop, anthEnd), "", false},
		{"anthropic_no_message_stop", "/messages", deepSeekRecoverySSE(anthText, anthStop), "", true},
		{"anthropic_no_stop_reason", "/messages", deepSeekRecoverySSE(anthText, anthEnd), "message_stop", true},
		{"anthropic_aborted", "/messages", deepSeekRecoverySSE(anthText, `{"type":"message_delta","delta":{"stop_reason":"aborted"}}`, anthEnd), "aborted", true},
		{"anthropic_error_event", "/messages", deepSeekRecoverySSE(anthText) + "event: error\ndata: {\"message\":\"failure\"}\n\n", "failure", true},
		{"responses_complete", "/responses", deepSeekRecoverySSE(rspText, rspEnd), "", false},
		{"responses_missing_terminal", "/responses", deepSeekRecoverySSE(rspText, "[DONE]"), "[DONE]", true},
		{"responses_failed", "/responses", deepSeekRecoverySSE(rspText, `{"type":"response.failed","response":{"status":"failed"}}`), "response.failed", true},
		{"responses_reasoning_only", "/responses", deepSeekRecoverySSE(`{"type":"response.reasoning_text.delta","delta":"thinking"}`, rspEnd), "response.completed", true},
		{"responses_length_answer", "/responses", deepSeekRecoverySSE(rspText, `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, a := deepSeekRecoveryFixture(t, tt.path, "text/event-stream", tt.raw)
			got, err := io.ReadAll(resp.Body)
			if tt.fail {
				require.Error(t, err)
				require.Error(t, a.Err())
				if tt.forbidden != "" {
					require.NotContains(t, string(got), tt.forbidden)
				}
			} else {
				require.NoError(t, err)
				require.NoError(t, a.Err())
				require.Equal(t, tt.raw, string(got))
				require.True(t, a.Outcome().Terminal)
			}
		})
	}
}

func TestDeepSeekRecoveryStreamedTools(t *testing.T) {
	for _, tt := range []struct{ path, raw string }{
		{"/chat/completions", deepSeekRecoverySSE(
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"lookup","arguments":"{"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, "[DONE]")},
		{"/messages", deepSeekRecoverySSE(
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"lookup","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"}"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`, `{"type":"message_stop"}`)},
		{"/responses", deepSeekRecoverySSE(
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"lookup","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}`,
			`{"type":"response.completed","response":{"status":"completed"}}`)},
	} {
		t.Run(tt.path, func(t *testing.T) {
			resp, a := deepSeekRecoveryFixture(t, tt.path, "text/event-stream", tt.raw)
			_, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.True(t, a.Outcome().ToolCall)
			require.False(t, a.Outcome().Answer)
		})
	}
}

func TestDeepSeekRecoveryUsageRemainsSafeOnFailure(t *testing.T) {
	resp, a := deepSeekRecoveryFixture(t, "/chat/completions", "text/event-stream", deepSeekRecoverySSE(
		`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_cache_hit_tokens":80,"completion_tokens_details":{"reasoning_tokens":19}}}`,
		`{"error":{"message":"secret upstream diagnostic"}}`))
	_, err := io.ReadAll(resp.Body)
	require.Error(t, err)
	u, unknown := a.ObservedUsage()
	require.False(t, unknown)
	require.Equal(t, int64(100), u.InputTokens)
	require.Equal(t, int64(20), u.OutputTokens)
	require.Equal(t, int64(80), u.CacheReadInputTokens)
	require.Equal(t, int64(19), u.ReasoningTokens)
	require.NotContains(t, fmt.Sprint(a.Outcome()), "secret")
	resp2, a2 := deepSeekRecoveryFixture(t, "/messages", "text/event-stream", deepSeekRecoverySSE(
		`{"type":"message_start","message":{"usage":{"input_tokens":3,"output_tokens":0,"cache_creation_input_tokens":10}}}`,
		`{"type":"message_delta","usage":{"output_tokens":9}}`))
	_, err = io.ReadAll(resp2.Body)
	require.Error(t, err)
	u, unknown = a2.ObservedUsage()
	require.False(t, unknown)
	require.Equal(t, int64(3), u.InputTokens)
	require.Equal(t, int64(9), u.OutputTokens)
	require.Equal(t, int64(10), u.CacheCreationInputTokens)
	resp3, a3 := deepSeekRecoveryFixture(t, "/responses", "application/json", `{"status":"failed","usage":{"output_tokens":2}}`)
	_, _ = io.ReadAll(resp3.Body)
	u, unknown = a3.ObservedUsage()
	require.True(t, unknown)
	require.Equal(t, int64(2), u.OutputTokens)
}

func TestDeepSeekRecoveryRejectsEveryHTTPErrorAndDuplicate(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 408, 409, 413, 422, 429, 499, 500, 502, 503, 504, 529, 599} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ctx, a := NewDeepSeekRecoveryAttempt(context.Background())
			defer a.Close()
			body := &deepSeekCloseObservedBody{ReadCloser: io.NopCloser(strings.NewReader("never read"))}
			u := &deepSeekRecoveryUpstreamStub{do: func(r *http.Request) (*http.Response, error) {
				require.Nil(t, r.GetBody, "physical transport must not be able to replay")
				require.True(t, HTTPUpstreamRedirectsDisabled(r.Context()))
				return &http.Response{StatusCode: status, Body: body, Header: http.Header{}}, nil
			}}
			s := &OpenAIGatewayService{httpUpstream: u}
			r := deepSeekRecoveryRequest(t, ctx, "/chat/completions")
			_, err := s.doOpenAIUpstream(r, "", &Account{ID: 7, Platform: "deepseek"})
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, status, failover.StatusCode)
			require.False(t, failover.RetryableOnSameAccount)
			require.Equal(t, int32(1), body.closed.Load())
			_, err = s.doOpenAIUpstream(r, "", &Account{ID: 7, Platform: "deepseek"})
			require.Error(t, err)
			require.Equal(t, int32(1), u.calls.Load())
		})
	}
}

func TestDeepSeekRecoveryRejectsAdaptiveResendAfterSuccessfulTransport(t *testing.T) {
	ctx, a := NewDeepSeekRecoveryAttempt(context.Background())
	defer a.Close()
	u := &deepSeekRecoveryUpstreamStub{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}
	s := &OpenAIGatewayService{httpUpstream: u}
	r := deepSeekRecoveryRequest(t, ctx, "/chat/completions")
	_, err := s.doOpenAIUpstream(r, "", &Account{ID: 7, Platform: "deepseek"})
	require.NoError(t, err)
	_, err = s.doOpenAIUpstream(r, "", &Account{ID: 7, Platform: "deepseek"})
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, GatewayFailureReason("deepseek_duplicate_submission"), failover.Reason)
	require.Equal(t, int32(1), u.calls.Load())
}

type deepSeekCloseObservedBody struct {
	io.ReadCloser
	closed atomic.Int32
}

func (b *deepSeekCloseObservedBody) Close() error { b.closed.Add(1); return b.ReadCloser.Close() }

func TestDeepSeekRecoveryRequiresExplicitGuardAndPlatform(t *testing.T) {
	ctx, a := NewDeepSeekRecoveryAttempt(context.Background())
	defer a.Close()
	for _, tt := range []struct {
		name, platform, path string
		ctx                  context.Context
	}{
		{"no_guard", "deepseek", "/chat/completions", context.Background()},
		{"other_platform", "openai", "/chat/completions", ctx},
		{"count_tokens", "deepseek", "/messages/count_tokens", ctx},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u := &deepSeekRecoveryUpstreamStub{do: func(r *http.Request) (*http.Response, error) {
				require.NotNil(t, r.GetBody)
				return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"original"}`))}, nil
			}}
			s := &OpenAIGatewayService{httpUpstream: u}
			resp, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, tt.ctx, tt.path), "", &Account{ID: 7, Platform: tt.platform})
			require.NoError(t, err)
			require.Equal(t, 400, resp.StatusCode)
			_ = resp.Body.Close()
			require.NoError(t, a.Err())
		})
	}
}

func TestDeepSeekRecoveryClientAndLeaseCancelActualHTTPRead(t *testing.T) {
	for _, mode := range []string{"client", "lease", "close"} {
		t.Run(mode, func(t *testing.T) {
			serverCanceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, deepSeekRecoverySSE(`{"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(serverCanceled)
			}))
			defer server.Close()
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			parent, releaseOwner := WithAPIKeyAdmissionOwner(parent)
			defer releaseOwner()
			ctx, a := NewDeepSeekRecoveryAttempt(parent)
			defer a.Close()
			var actualCtx context.Context
			var observed *deepSeekCloseObservedBody
			u := &deepSeekRecoveryUpstreamStub{do: func(r *http.Request) (*http.Response, error) {
				actualCtx = r.Context()
				resp, err := server.Client().Do(r)
				if err == nil {
					observed = &deepSeekCloseObservedBody{ReadCloser: resp.Body}
					resp.Body = observed
				}
				return resp, err
			}}
			s := &OpenAIGatewayService{httpUpstream: u}
			r := deepSeekRecoveryRequest(t, ctx, "/chat/completions")
			target, _ := http.NewRequest(http.MethodPost, server.URL+"/chat/completions", nil)
			r.URL = target.URL
			resp, err := s.doOpenAIUpstream(r, "", &Account{ID: 7, Platform: "deepseek"})
			require.NoError(t, err)
			defer resp.Body.Close()
			buf := make([]byte, 1024)
			n, err := resp.Body.Read(buf)
			require.NoError(t, err)
			require.Contains(t, string(buf[:n]), "hello", "a semantic frame must stream before upstream finishes")
			readDone := make(chan error, 1)
			go func() { _, e := io.ReadAll(resp.Body); readDone <- e }()
			switch mode {
			case "client":
				cancel()
			case "lease":
				owner, _ := apiKeyAdmissionOwnerFromContext(ctx)
				owner.cancel(ErrAPIKeySlotLeaseLost)
			case "close":
				a.Close()
			}
			select {
			case <-actualCtx.Done():
			case <-time.After(time.Second):
				t.Fatal("real transport context was not cancelled")
			}
			select {
			case <-serverCanceled:
			case <-time.After(time.Second):
				t.Fatal("remote HTTP handler did not observe cancellation")
			}
			select {
			case err = <-readDone:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("body Read did not stop")
			}
			require.Eventually(t, func() bool { return observed.closed.Load() == 1 }, time.Second, time.Millisecond)
			if mode == "client" {
				require.ErrorIs(t, a.Err(), context.Canceled)
			}
			if mode == "lease" {
				require.ErrorIs(t, a.Err(), ErrAPIKeySlotLeaseLost)
			}
		})
	}
}

func TestDeepSeekRecoveryBudgetsCannotBeExtendedByPings(t *testing.T) {
	for _, mode := range []string{"first_semantic_timeout", "stall_timeout", "attempt_timeout"} {
		t.Run(mode, func(t *testing.T) {
			limits := deepSeekRecoveryTimeouts{time.Second, time.Second, time.Second}
			switch mode {
			case "first_semantic_timeout":
				limits.firstSemantic = 70 * time.Millisecond
			case "stall_timeout":
				limits.stall = 70 * time.Millisecond
			case "attempt_timeout":
				limits.attempt = 100 * time.Millisecond
			}
			ctx, a := newDeepSeekRecoveryAttempt(context.Background(), limits)
			defer a.Close()
			pr, pw := io.Pipe()
			writerDone := make(chan struct{})
			go func() {
				defer close(writerDone)
				defer pw.Close()
				if mode != "first_semantic_timeout" {
					if _, e := io.WriteString(pw, deepSeekRecoverySSE(`{"choices":[{"delta":{"content":"hello"}}]}`)); e != nil {
						return
					}
				}
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						frame := ": ping\n\n"
						if mode == "attempt_timeout" {
							frame = deepSeekRecoverySSE(`{"choices":[{"delta":{"content":"more"}}]}`)
						}
						if _, e := io.WriteString(pw, frame); e != nil {
							return
						}
					}
				}
			}()
			u := &deepSeekRecoveryUpstreamStub{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}, nil
			}}
			s := &OpenAIGatewayService{httpUpstream: u}
			resp, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, ctx, "/chat/completions"), "", &Account{ID: 7, Platform: "deepseek"})
			require.NoError(t, err)
			defer resp.Body.Close()
			finished := make(chan error, 1)
			go func() { _, e := io.ReadAll(resp.Body); finished <- e }()
			select {
			case err = <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("deadline did not stop body")
			}
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, GatewayFailureReason("deepseek_"+mode), failover.Reason)
			<-writerDone
		})
	}
}

func TestDeepSeekRecoveryFirstSemanticTimeoutCancelsHeaderWait(t *testing.T) {
	ctx, a := newDeepSeekRecoveryAttempt(context.Background(), deepSeekRecoveryTimeouts{30 * time.Millisecond, time.Second, time.Second})
	defer a.Close()
	u := &deepSeekRecoveryUpstreamStub{do: func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }}
	s := &OpenAIGatewayService{httpUpstream: u}
	_, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, ctx, "/chat/completions"), "", &Account{ID: 7, Platform: "deepseek"})
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, GatewayFailureReason("deepseek_first_semantic_timeout"), failover.Reason)
}

func TestDeepSeekRecoveryConcurrentCloseIsIdempotent(t *testing.T) {
	ctx, a := NewDeepSeekRecoveryAttempt(context.Background())
	body := &deepSeekCloseObservedBody{ReadCloser: io.NopCloser(strings.NewReader(""))}
	u := &deepSeekRecoveryUpstreamStub{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
	}}
	s := &OpenAIGatewayService{httpUpstream: u}
	_, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, ctx, "/chat/completions"), "", &Account{ID: 7, Platform: "deepseek"})
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.Close() }()
	}
	wg.Wait()
	require.Equal(t, int32(1), body.closed.Load())
	if err := a.Err(); err != nil {
		require.True(t, errors.Is(err, context.Canceled))
	}
}

func TestDeepSeekRecoveryCyberPolicyHTTP(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ctx, a := NewDeepSeekRecoveryAttempt(context.Background())
			defer a.Close()
			payload := `{"error":{"code":" CYBER_POLICY ","message":"sk-test-audit-secret request details"},"usage":{"prompt_tokens":29,"completion_tokens":7}}`
			body := &deepSeekCloseObservedBody{ReadCloser: io.NopCloser(strings.NewReader(payload))}
			u := &deepSeekRecoveryUpstreamStub{do: func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
			}}
			s := &OpenAIGatewayService{httpUpstream: u}
			resp, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, ctx, "/chat/completions"), "", &Account{ID: 7, Platform: "deepseek"})
			require.Nil(t, resp)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, status, failover.StatusCode)
			require.True(t, failover.ShouldRetryNextAccount())
			require.False(t, failover.RetryableOnSameAccount)
			require.Empty(t, failover.ResponseBody)
			require.Equal(t, int32(1), body.closed.Load())
			out := a.Outcome()
			require.NotNil(t, out.CyberPolicy)
			require.Equal(t, CyberPolicyMark{Code: "cyber_policy", Message: deepSeekCyberPolicyMessage,
				UpstreamStatus: status, UpstreamInTok: 29, UpstreamOutTok: 7}, *out.CyberPolicy)
			require.False(t, out.UsageUnknown)
			serialized, err := json.Marshal(out)
			require.NoError(t, err)
			require.NotContains(t, string(serialized), "sk-test-audit-secret")
			require.NotContains(t, string(serialized), "request details")
			out.CyberPolicy.Message = "caller mutation"
			require.Equal(t, deepSeekCyberPolicyMessage, a.Outcome().CyberPolicy.Message)
		})
	}
}

func TestDeepSeekRecoveryCyberPolicySourceErrors(t *testing.T) {
	for _, tt := range []struct {
		name, path, contentType, raw string
	}{
		{"json_error", "/chat/completions", "application/json", `{"error":{"code":"cyber_policy","message":"private request"},"usage":{"prompt_tokens":12,"completion_tokens":3}}`},
		{"sse_error_event", "/messages", "text/event-stream", "event: error\ndata: " + `{"type":"error","error":{"code":"cyber_policy","message":"private request"},"usage":{"input_tokens":12,"output_tokens":3}}` + "\n\n"},
		{"sse_response_failed_type", "/responses", "text/event-stream", deepSeekRecoverySSE(`{"type":"response.failed","response":{"error":{"code":"cyber_policy","message":"private request"},"usage":{"input_tokens":12,"output_tokens":3}}}`)},
		{"sse_response_failed_event", "/responses", "text/event-stream", "event: response.failed\ndata: " + `{"response":{"error":{"code":"cyber_policy","message":"private request"},"usage":{"input_tokens":12,"output_tokens":3}}}` + "\n\n"},
		{"sse_prior_usage", "/chat/completions", "text/event-stream", deepSeekRecoverySSE(`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`, `{"error":{"code":"cyber_policy","message":"private request"}}`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp, a := deepSeekRecoveryFixture(t, tt.path, tt.contentType, tt.raw)
			got, err := io.ReadAll(resp.Body)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.True(t, failover.ShouldRetryNextAccount())
			require.False(t, failover.RetryableOnSameAccount)
			require.NotContains(t, string(got), "cyber_policy", "failed frame must not reach the converter")
			require.NotContains(t, string(got), "private request")
			require.Error(t, a.Err())
			out := a.Outcome()
			require.NotNil(t, out.CyberPolicy)
			require.Equal(t, CyberPolicyMark{Code: "cyber_policy", Message: deepSeekCyberPolicyMessage,
				UpstreamStatus: 200, UpstreamInTok: 12, UpstreamOutTok: 3}, *out.CyberPolicy)
			require.False(t, out.UsageUnknown)
			require.False(t, out.Terminal)
		})
	}
	resp, a := deepSeekRecoveryFixture(t, "/chat/completions", "application/json", `{"error":{"code":"content_policy","message":"text says cyber_policy"}}`)
	_, err := io.ReadAll(resp.Body)
	require.Error(t, err)
	require.Nil(t, a.Outcome().CyberPolicy, "audit must use exact code detection, not message matching")
	require.True(t, a.Outcome().UsageUnknown)
}

// This server deliberately never ends the body. Complete JSON must be observed
// without waiting for EOF; an incomplete/blocked body must be cancelled within
// the short audit budget, including when normal attempt limits are minutes.
func TestDeepSeekRecoveryCyberPolicyHTTPBodyDeadline(t *testing.T) {
	for _, mode := range []string{"complete_without_eof", "blocked_body", "parent_cancel"} {
		t.Run(mode, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentHeaders, serverCancelled := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				if mode == "complete_without_eof" {
					_, _ = io.WriteString(w, `{"error":{"code":"cyber_policy","message":"private"},"usage":{"prompt_tokens":2,"completion_tokens":0}}`)
				} else {
					_, _ = io.WriteString(w, `{"error":`)
				}
				w.(http.Flusher).Flush()
				close(sentHeaders)
				<-r.Context().Done()
				close(serverCancelled)
			}))
			defer server.Close()
			ctx, a := NewDeepSeekRecoveryAttempt(parent)
			defer a.Close()
			u := &deepSeekRecoveryUpstreamStub{do: server.Client().Do}
			s := &OpenAIGatewayService{httpUpstream: u}
			r := deepSeekRecoveryRequest(t, ctx, "/chat/completions")
			target, err := http.NewRequest(http.MethodPost, server.URL+"/chat/completions", nil)
			require.NoError(t, err)
			r.URL = target.URL
			result := make(chan error, 1)
			go func() {
				_, e := s.doOpenAIUpstream(r, "", &Account{ID: 7, Platform: "deepseek"})
				result <- e
			}()
			select {
			case <-sentHeaders:
			case <-time.After(time.Second):
				t.Fatal("error headers not received")
			}
			if mode == "parent_cancel" {
				cancel()
			}
			select {
			case err = <-result:
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP error body exceeded bounded read deadline")
			}
			if mode == "parent_cancel" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, http.StatusForbidden, failover.StatusCode)
				require.True(t, failover.ShouldRetryNextAccount())
			}
			select {
			case <-serverCancelled:
			case <-time.After(time.Second):
				t.Fatal("actual HTTP request/body was not cancelled")
			}
			if mode == "complete_without_eof" {
				require.NotNil(t, a.Outcome().CyberPolicy)
				require.False(t, a.Outcome().UsageUnknown, "explicit zero output is observed, not inferred")
			} else {
				require.Nil(t, a.Outcome().CyberPolicy)
				require.True(t, a.Outcome().UsageUnknown)
			}
		})
	}
}

type deepSeekCountingErrorBody struct {
	io.ReadCloser
	read int
}

func (b *deepSeekCountingErrorBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += n
	return n, err
}

func TestDeepSeekRecoveryCyberPolicyHTTPBodySizeBound(t *testing.T) {
	ctx, a := NewDeepSeekRecoveryAttempt(context.Background())
	defer a.Close()
	payload := `{"error":{"code":"cyber_policy","message":"` + strings.Repeat("x", deepSeekMaxErrorBody*2) + `"}}`
	body := &deepSeekCountingErrorBody{ReadCloser: io.NopCloser(strings.NewReader(payload))}
	u := &deepSeekRecoveryUpstreamStub{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}, Body: body}, nil
	}}
	s := &OpenAIGatewayService{httpUpstream: u}
	_, err := s.doOpenAIUpstream(deepSeekRecoveryRequest(t, ctx, "/chat/completions"), "", &Account{ID: 7, Platform: "deepseek"})
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusBadRequest, failover.StatusCode)
	require.LessOrEqual(t, body.read, deepSeekMaxErrorBody)
	require.Nil(t, a.Outcome().CyberPolicy, "an incomplete oversized JSON document is not audit evidence")
	require.True(t, a.Outcome().UsageUnknown)
}
