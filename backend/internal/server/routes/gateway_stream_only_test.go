package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestGatewayRoutesGroupStreamOnlyRejectsNonStreamRequests 走真实路由表：非流式请求在调度前被拒绝，
// 合成分组也在改写之前就拒绝。
func TestGatewayRoutesGroupStreamOnlyRejectsNonStreamRequests(t *testing.T) {
	// Exercise each mounted route once. Composite routes retain pre-rewrite
	// rejection; request-value permutations belong to the middleware tests.
	for _, tc := range []struct{ platform, path, body, want string }{
		{service.PlatformAnthropic, "/v1/messages", `{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[]}`, `"type":"error"`},
		{service.PlatformAnthropic, "/antigravity/v1/messages", `{"model":"claude-sonnet-4-5","stream":false}`, `"type":"error"`},
		{service.PlatformOpenAI, "/v1/chat/completions", `{"model":"gpt-5.4","stream":false,"messages":[]}`, "stream_required"},
		{service.PlatformComposite, "/chat/completions", `{"model":"gpt-5.4","messages":[]}`, "stream_required"},
		{service.PlatformOpenAI, "/v1/responses", `{"model":"gpt-5.4","input":"hi"}`, "stream_required"},
		{service.PlatformComposite, "/responses", `{"model":"gpt-5.4","stream":"true"}`, "stream_required"},
		{service.PlatformOpenAI, "/backend-api/codex/responses", `{"model":"gpt-5.4","stream":true,"stream":false}`, "stream_required"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			router := newGatewayRoutesTestRouterWithGroup(&service.Group{Platform: tc.platform, StreamOnly: true})
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), tc.want)
		})
	}
}
