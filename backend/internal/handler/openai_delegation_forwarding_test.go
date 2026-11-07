//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the actual HTTP handler and OAuth forwarding service: a server-side
// search in replayed history must not make the delegated task disappear upstream.
func TestOpenAIResponsesDelegationAfterWebSearchReachesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ tool string }{
		{tool: "create_thread"},
		{tool: "send_message_to_thread"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			upstream := &excelBPSFailoverUpstream{answer: func(int) *http.Response { return excelBPSCompleted() }}
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			c, rec := newOpenAIResponsesFailoverTestContext(t, context.Background())
			request := map[string]any{
				"model": "gpt-6-astra", "stream": false,
				"input": []any{
					map[string]any{"type": "message", "role": "user", "content": "workspace rules"},
					map[string]any{"type": "function_call_output", "id": "fco_delegation", "namespace": "codex_app", "name": tc.tool, "output": delegationEnvelope},
					map[string]any{"type": "web_search_call", "id": "ws_history", "status": "completed", "action": map[string]any{"type": "search", "query": "existing component"}},
					map[string]any{"type": "custom_tool_call", "call_id": "call_read", "name": "exec", "input": "read source"},
					map[string]any{"type": "custom_tool_call_output", "call_id": "call_read", "output": "ordinary tool result"},
				},
			}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
			c.Request.ContentLength = int64(len(body))

			h.Responses(c)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, upstream.bodies, 1, "inspect the body actually sent by the service")
			input := gjson.GetBytes(upstream.bodies[0], "input").Array()
			require.Len(t, input, 5)
			require.Equal(t, "user", input[1].Get("role").String())
			require.Equal(t, delegationEnvelope, input[1].Get("content.0.text").String())
			require.Equal(t, "web_search_call", input[2].Get("type").String())
			require.Equal(t, "ws_history", input[2].Get("id").String())
			require.Equal(t, "custom_tool_call", input[3].Get("type").String())
			require.Equal(t, "custom_tool_call_output", input[4].Get("type").String())
			require.NotEmpty(t, input[3].Get("call_id").String())
			require.Equal(t, input[3].Get("call_id").String(), input[4].Get("call_id").String(), "custom tool pairing survives service ID normalization")
			require.Equal(t, "ordinary tool result", input[4].Get("output").String())
			require.False(t, input[4].Get("role").Exists(), "ordinary tool output must not gain user authority")
		})
	}
}
