//go:build unit

package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func recoverySinkTestWriter(t *testing.T, protocol string, stream bool) (*deepSeekRecoveryStream, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	w := newDeepSeekRecoveryStream(ctx.Writer, protocol, stream)
	require.NoError(t, w.BeginAttempt())
	return w, rec
}
func recoverySinkWrite(t *testing.T, w *deepSeekRecoveryStream, s string) {
	t.Helper()
	n, err := w.Write([]byte(s))
	require.NoError(t, err)
	require.Equal(t, len(s), n)
}
func recoverySinkChat(delta map[string]any, finish any, usage any) string {
	obj := map[string]any{"id": "vendor-id", "object": "chat.completion.chunk", "created": 123, "model": "deepseek-flash", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	if usage != nil {
		obj["usage"] = usage
	}
	b, _ := json.Marshal(obj)
	return "data: " + string(b) + "\n\n"
}
func recoverySinkMessage(kind string, fields map[string]any) string {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = kind
	b, _ := json.Marshal(fields)
	return "event: " + kind + "\ndata: " + string(b) + "\n\n"
}
func recoverySinkMessageStart(input int) string {
	return recoverySinkMessage("message_start", map[string]any{"message": map[string]any{"id": "vendor-msg", "type": "message", "role": "assistant", "model": "deepseek-flash", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": input, "output_tokens": 0}}})
}
func recoverySinkBlockStart(index int, kind string) string {
	content := map[string]any{"type": kind}
	switch kind {
	case "text":
		content["text"] = ""
	case "thinking":
		content["thinking"] = ""
	case "tool_use":
		content["id"] = "call_1"
		content["name"] = "lookup"
		content["input"] = map[string]any{}
	}
	return recoverySinkMessage("content_block_start", map[string]any{"index": index, "content_block": content})
}
func recoverySinkBlockDelta(index int, kind, key, text string) string {
	return recoverySinkMessage("content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": kind, key: text}})
}
func recoverySinkBlockStop(index int) string {
	return recoverySinkMessage("content_block_stop", map[string]any{"index": index})
}
func recoverySinkMessageEnd(output int, reason string) string {
	return recoverySinkMessage("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": output}}) + recoverySinkMessage("message_stop", nil)
}
func recoverySinkFrames(t *testing.T, s string) []map[string]any {
	t.Helper()
	var frames []map[string]any
	for _, event := range strings.Split(s, "\n\n") {
		for _, line := range strings.Split(event, "\n") {
			if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
				continue
			}
			var obj map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &obj))
			frames = append(frames, obj)
		}
	}
	return frames
}

func TestDeepSeekRecoverySinkChatRealtimeRetryAndTerminal(t *testing.T) {
	w, rec := recoverySinkTestWriter(t, "chat", true)
	w.Header().Set("X-Provider-Secret", "never-forward")
	w.Header().Set("Content-Type", "unsafe/type")
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"role": "assistant", "content": ""}, nil, nil))
	require.Empty(t, rec.Body.String())
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"reasoning_content": "thinking-A"}, nil, nil))
	require.Contains(t, rec.Body.String(), "thinking-A")
	require.True(t, rec.Flushed)
	require.True(t, w.BusinessStarted())
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"content": "answer-A"}, "stop", map[string]any{"total_tokens": 999}))
	recoverySinkWrite(t, w, "data: [DONE]\n\n")
	require.NotContains(t, rec.Body.String(), `"finish_reason":"stop"`)
	require.NotContains(t, rec.Body.String(), `"usage"`)
	require.NotContains(t, rec.Body.String(), "[DONE]")
	require.Error(t, w.EndAttempt(errors.New("source guard rejected incomplete upstream")))
	require.True(t, w.CanRetry())
	require.NoError(t, w.BeginAttempt())
	require.Equal(t, -1, w.Size())
	require.False(t, w.Written())
	require.NotContains(t, rec.Body.String(), "正在重新生成")
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"role": "assistant"}, nil, nil))
	require.NotContains(t, rec.Body.String(), "正在重新生成")
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"reasoning_content": "thinking-B"}, nil, nil))
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"content": "answer-B"}, nil, nil))
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{}, "stop", map[string]any{"total_tokens": 7}))
	// A valid finish without DONE is compatible with existing upstreams.
	require.NoError(t, w.EndAttempt(nil))
	require.NoError(t, w.CommitSuccess())
	require.Equal(t, 1, strings.Count(rec.Body.String(), "[DONE]"))
	require.Equal(t, 1, strings.Count(rec.Body.String(), "正在重新生成"))
	require.Equal(t, 1, strings.Count(rec.Body.String(), `"finish_reason":"stop"`))
	require.NotContains(t, rec.Body.String(), "999")
	require.Contains(t, rec.Body.String(), `"total_tokens":7`)
	frames := recoverySinkFrames(t, rec.Body.String())
	id := frames[0]["id"]
	for _, f := range frames {
		require.Equal(t, id, f["id"])
	}
	require.NotEqual(t, "vendor-id", id)
	require.Empty(t, rec.Header().Get("X-Provider-Secret"))
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Error(t, w.CommitSuccess())
	require.False(t, w.CanRetry())
}

func TestDeepSeekRecoverySinkMessagesRemapsBlocksAcrossRetries(t *testing.T) {
	w, rec := recoverySinkTestWriter(t, "messages", true)
	recoverySinkWrite(t, w, recoverySinkMessageStart(900)+recoverySinkBlockStart(0, "thinking"))
	require.Empty(t, rec.Body.String())
	recoverySinkWrite(t, w, recoverySinkBlockDelta(0, "thinking_delta", "thinking", "thought-A"))
	require.Contains(t, rec.Body.String(), "thought-A")
	recoverySinkWrite(t, w, recoverySinkMessage("error", map[string]any{"error": map[string]any{"type": "api_error", "message": "private-vendor-error"}}))
	require.NotContains(t, rec.Body.String(), "private-vendor-error")
	require.Error(t, w.EndAttempt(nil))
	require.True(t, w.CanRetry())
	require.NoError(t, w.BeginAttempt())
	require.Contains(t, rec.Body.String(), `"type":"content_block_stop"`)
	require.NotContains(t, rec.Body.String(), "正在重新生成")
	recoverySinkWrite(t, w, recoverySinkMessageStart(12)+recoverySinkBlockStart(0, "thinking"))
	require.NotContains(t, rec.Body.String(), "正在重新生成")
	recoverySinkWrite(t, w, recoverySinkBlockDelta(0, "thinking_delta", "thinking", "thought-B")+recoverySinkBlockDelta(0, "signature_delta", "signature", "sig-B")+recoverySinkBlockStop(0))
	recoverySinkWrite(t, w, recoverySinkBlockStart(1, "text")+recoverySinkBlockDelta(1, "text_delta", "text", "answer-B")+recoverySinkBlockStop(1)+recoverySinkMessageEnd(8, "end_turn"))
	require.Contains(t, rec.Body.String(), "answer-B")
	require.NotContains(t, rec.Body.String(), "event: message_delta")
	require.NotContains(t, rec.Body.String(), "event: message_stop")
	require.NoError(t, w.EndAttempt(nil))
	require.NoError(t, w.CommitSuccess())
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_start"))
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_stop"))
	require.Equal(t, 1, strings.Count(rec.Body.String(), "正在重新生成"))
	frames := recoverySinkFrames(t, rec.Body.String())
	next := 0
	open := map[int]bool{}
	for _, f := range frames {
		switch f["type"] {
		case "content_block_start":
			index := int(f["index"].(float64))
			require.Equal(t, next, index)
			next++
			open[index] = true
		case "content_block_delta":
			require.True(t, open[int(f["index"].(float64))])
		case "content_block_stop":
			index := int(f["index"].(float64))
			require.True(t, open[index])
			delete(open, index)
		case "message_delta":
			usage := f["usage"].(map[string]any)
			require.Equal(t, float64(12), usage["input_tokens"])
			require.Equal(t, float64(8), usage["output_tokens"])
		}
	}
	require.Empty(t, open)
	require.Equal(t, 4, next)
	require.NotContains(t, rec.Body.String(), "900")
}

func TestDeepSeekRecoverySinkFragmentedSSE(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		for _, newline := range []string{"\n", "\r\n", "\r"} {
			t.Run(protocol+strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
				w, rec := recoverySinkTestWriter(t, protocol, true)
				input := recoverySinkChat(map[string]any{"content": "文本🙂"}, nil, nil) + recoverySinkChat(map[string]any{}, "stop", nil) + "data: [DONE]\n\n"
				if protocol == "messages" {
					input = recoverySinkMessageStart(2) + recoverySinkBlockStart(7, "text") + recoverySinkBlockDelta(7, "text_delta", "text", "文本🙂") + recoverySinkBlockStop(7) + recoverySinkMessageEnd(3, "end_turn")
				}
				input = strings.ReplaceAll(input, "\n", newline)
				for i := range len(input) {
					recoverySinkWrite(t, w, input[i:i+1])
				}
				require.Contains(t, rec.Body.String(), "文本🙂")
				require.NoError(t, w.EndAttempt(nil))
				require.NoError(t, w.CommitSuccess())
			})
		}
	}
	t.Run("multiline_data", func(t *testing.T) {
		w, rec := recoverySinkTestWriter(t, "chat", true)
		recoverySinkWrite(t, w, "data: {\n"+"data: \"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		require.Contains(t, rec.Body.String(), "hello")
		require.NoError(t, w.EndAttempt(nil))
		require.NoError(t, w.CommitSuccess())
	})
}

func TestDeepSeekRecoverySinkToolExposureStopsRetry(t *testing.T) {
	for _, tc := range []struct{ name, protocol, input string }{
		{"chat_tools", "chat", recoverySinkChat(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{\"q\":"}}}}, nil, nil)},
		{"legacy_function", "chat", recoverySinkChat(map[string]any{"function_call": map[string]any{"name": "lookup", "arguments": "{"}}, nil, nil)},
		{"messages_tool", "messages", recoverySinkMessageStart(1) + recoverySinkBlockStart(0, "tool_use") + recoverySinkBlockDelta(0, "input_json_delta", "partial_json", "{\"q\":")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, rec := recoverySinkTestWriter(t, tc.protocol, true)
			recoverySinkWrite(t, w, tc.input)
			require.True(t, w.ToolExposed())
			require.NotEmpty(t, rec.Body.String())
			require.Error(t, w.EndAttempt(errors.New("aborted")))
			require.False(t, w.CanRetry())
			require.Error(t, w.BeginAttempt())
			require.NoError(t, w.CommitFailure("failed"))
			require.NotContains(t, rec.Body.String(), "[DONE]")
			require.NotContains(t, rec.Body.String(), "message_stop")
			require.NotContains(t, rec.Body.String(), "content_block_stop")
			require.Contains(t, rec.Body.String(), "failed")
		})
	}
}

func TestDeepSeekRecoverySinkSuppressesHTTPAndProtocolErrors(t *testing.T) {
	for _, stream := range []bool{true, false} {
		w, rec := recoverySinkTestWriter(t, "chat", stream)
		w.Header().Set("X-Provider", "hidden")
		w.WriteHeader(http.StatusUnauthorized)
		w.WriteHeaderNow()
		recoverySinkWrite(t, w, `{"error":{"message":"secret-from-vendor"}}`)
		w.Flush()
		require.Empty(t, rec.Body.String())
		require.False(t, w.downstream.Written())
		require.Error(t, w.EndAttempt(nil))
		require.True(t, w.CanRetry())
		require.NoError(t, w.BeginAttempt())
		if stream {
			recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"content": "ok"}, "stop", nil))
		} else {
			recoverySinkWrite(t, w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
		}
		require.NoError(t, w.EndAttempt(nil))
		require.NoError(t, w.CommitSuccess())
		require.Equal(t, 200, rec.Code)
		require.NotContains(t, rec.Body.String(), "secret-from-vendor")
		require.Empty(t, rec.Header().Get("X-Provider"))
	}
	for _, protocol := range []string{"chat", "messages"} {
		t.Run(protocol+"_stream_error", func(t *testing.T) {
			w, rec := recoverySinkTestWriter(t, protocol, true)
			payload := "data: {\"error\":{\"message\":\"private\"}}\n\n"
			if protocol == "messages" {
				payload = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"private\"}}\n\n"
			}
			recoverySinkWrite(t, w, payload)
			require.Empty(t, rec.Body.String())
			require.Error(t, w.EndAttempt(nil))
			require.NoError(t, w.CommitFailure("public"))
			require.Equal(t, 502, rec.Code)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			require.NotContains(t, rec.Body.String(), "private")
		})
	}
}

func TestDeepSeekRecoverySinkNonstreamBuffersWinner(t *testing.T) {
	w, rec := recoverySinkTestWriter(t, "messages", false)
	require.NoError(t, w.Heartbeat())
	require.Empty(t, rec.Body.String())
	recoverySinkWrite(t, w, `{"type":"error","error":{"type":"api_error","message":"private"}}`)
	require.Error(t, w.EndAttempt(nil))
	require.Empty(t, rec.Body.String())
	require.NoError(t, w.BeginAttempt())
	body := `{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"foo","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":2,"output_tokens":3}}`
	recoverySinkWrite(t, w, body)
	require.NoError(t, w.EndAttempt(nil))
	require.False(t, w.ToolExposed())
	require.Empty(t, rec.Body.String())
	require.NoError(t, w.CommitSuccess())
	require.True(t, w.ToolExposed())
	require.Equal(t, body, rec.Body.String())
	require.False(t, w.CanRetry())
}

func TestDeepSeekRecoverySinkInvalidOrMissingTerminal(t *testing.T) {
	for _, tc := range []struct{ name, protocol, input string }{
		{"done_only", "chat", "data: [DONE]\n\n"},
		{"usage_only", "chat", "data: {\"choices\":[],\"usage\":{\"total_tokens\":2}}\n\n"},
		{"text_only", "chat", recoverySinkChat(map[string]any{"content": "hello"}, nil, nil)},
		{"malformed", "chat", "data: {broken}\n\n"},
		{"incomplete_event", "chat", strings.TrimSuffix(recoverySinkChat(map[string]any{"content": "hello"}, "stop", nil), "\n")},
		{"resource_error", "chat", recoverySinkChat(map[string]any{}, "insufficient_system_resource", nil)},
		{"tool_end_without_tool", "chat", recoverySinkChat(map[string]any{}, "tool_calls", nil)},
		{"messages_missing_delta", "messages", recoverySinkMessageStart(1) + recoverySinkMessage("message_stop", nil)},
		{"messages_open_block", "messages", recoverySinkMessageStart(1) + recoverySinkBlockStart(0, "text") + recoverySinkBlockDelta(0, "text_delta", "text", "x") + recoverySinkMessageEnd(1, "end_turn")},
		{"messages_unknown_index", "messages", recoverySinkMessageStart(1) + recoverySinkBlockDelta(0, "text_delta", "text", "x")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, rec := recoverySinkTestWriter(t, tc.protocol, true)
			recoverySinkWrite(t, w, tc.input)
			require.Error(t, w.EndAttempt(nil))
			require.Error(t, w.CommitSuccess())
			require.NotContains(t, rec.Body.String(), "[DONE]")
			require.NotContains(t, rec.Body.String(), "message_stop")
		})
	}
}

func TestDeepSeekRecoverySinkBoundedBuffers(t *testing.T) {
	t.Run("event", func(t *testing.T) {
		w, _ := recoverySinkTestWriter(t, "chat", true)
		recoverySinkWrite(t, w, "data: "+strings.Repeat("x", deepSeekRecoveryEventLimit))
		require.ErrorContains(t, w.EndAttempt(nil), "event too large")
		require.LessOrEqual(t, len(w.line), deepSeekRecoveryEventLimit)
	})
	t.Run("prelude", func(t *testing.T) {
		w, rec := recoverySinkTestWriter(t, "chat", true)
		for i := 0; i < 600; i++ {
			recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"role": "assistant"}, nil, nil))
		}
		require.ErrorContains(t, w.EndAttempt(nil), "prelude too large")
		require.Empty(t, rec.Body.String())
		require.LessOrEqual(t, w.preludeBytes, deepSeekRecoveryPreludeLimit)
	})
	t.Run("nonstream", func(t *testing.T) {
		w, _ := recoverySinkTestWriter(t, "chat", false)
		recoverySinkWrite(t, w, strings.Repeat("x", deepSeekRecoveryBodyLimit+1))
		require.ErrorContains(t, w.EndAttempt(nil), "response too large")
		require.LessOrEqual(t, w.body.Len(), deepSeekRecoveryBodyLimit)
	})
	t.Run("stream_not_buffered", func(t *testing.T) {
		w, rec := recoverySinkTestWriter(t, "chat", true)
		for i := 0; i < 50; i++ {
			recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"content": strings.Repeat("x", 4096)}, nil, nil))
		}
		require.Greater(t, rec.Body.Len(), deepSeekRecoveryPreludeLimit)
		require.Zero(t, w.body.Len())
		require.Empty(t, w.prelude)
		require.Empty(t, w.terminal)
	})
}

type recoverySinkBrokenWriter struct {
	gin.ResponseWriter
	writes int
	short  bool
}

func (w *recoverySinkBrokenWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.short {
		return len(p) - 1, nil
	}
	return 0, io.ErrClosedPipe
}
func TestDeepSeekRecoverySinkWriteFailureDisablesRetry(t *testing.T) {
	for _, tool := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			broken := &recoverySinkBrokenWriter{ResponseWriter: c.Writer, short: short}
			w := newDeepSeekRecoveryStream(broken, "chat", true)
			require.NoError(t, w.BeginAttempt())
			delta := map[string]any{"content": "hello"}
			if tool {
				delta = map[string]any{"function_call": map[string]any{"name": "f", "arguments": "{"}}
			}
			_, err := w.Write([]byte(recoverySinkChat(delta, nil, nil)))
			require.Error(t, err)
			require.True(t, w.ClientGone())
			require.Equal(t, tool, w.ToolExposed())
			require.Error(t, w.EndAttempt(err))
			require.False(t, w.CanRetry())
			require.Error(t, w.CommitFailure("no more write"))
			require.Equal(t, 1, broken.writes)
		}
	}
}

func TestDeepSeekRecoverySinkHeartbeatsAndFinalErrors(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		w, rec := recoverySinkTestWriter(t, protocol, true)
		require.NoError(t, w.Heartbeat())
		require.Contains(t, rec.Body.String(), ": keep-alive\n\n")
		require.False(t, w.Written())
		require.Equal(t, -1, w.Size())
		require.False(t, w.BusinessStarted())
		w.WriteHeader(http.StatusBadRequest)
		recoverySinkWrite(t, w, "private")
		require.Error(t, w.EndAttempt(nil))
		require.NoError(t, w.CommitFailure("public"))
		require.Equal(t, 200, rec.Code)
		require.NotContains(t, rec.Body.String(), "private")
		require.Contains(t, rec.Body.String(), "public")
		if protocol == "messages" {
			require.Contains(t, rec.Body.String(), "event: error")
		} else {
			require.NotContains(t, rec.Body.String(), "[DONE]")
		}
		before := rec.Body.String()
		require.NoError(t, w.Heartbeat())
		require.Equal(t, before, rec.Body.String())
	}
}

func TestDeepSeekRecoverySinkSerializesHeartbeatWithFrames(t *testing.T) {
	w, rec := recoverySinkTestWriter(t, "chat", true)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = w.Heartbeat()
			_ = w.BusinessStarted()
			_ = w.Size()
		}
	}()
	for i := 0; i < 100; i++ {
		recoverySinkWrite(t, w, recoverySinkChat(map[string]any{"content": "x"}, nil, nil))
	}
	recoverySinkWrite(t, w, recoverySinkChat(map[string]any{}, "stop", nil))
	require.NoError(t, w.EndAttempt(nil))
	wg.Wait()
	require.NoError(t, w.CommitSuccess())
	require.Len(t, recoverySinkFrames(t, rec.Body.String()), 101)
}
