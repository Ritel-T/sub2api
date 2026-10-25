package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/deepseektools"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDeepSeekToolsFetchExcludesPATBeforeForward(t *testing.T) {
	native := &service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "fixture-oauth-token", "chatgpt_account_id": "fixture-account"}}
	pat := &service.Account{ID: 43, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "at-fixture-only", "auth_mode": service.OpenAIAuthModePersonalAccessToken, "chatgpt_account_id": "fixture-account"}}
	require.True(t, pat.IsOpenAIPersonalAccessToken(), "the rejected fixture must exercise PAT detection")
	fetch := &deepSeekToolsBridge{request: &deepseektools.Request{Operation: "fetch"}, groupID: 12}
	search := &deepSeekToolsBridge{request: &deepseektools.Request{Operation: "search"}, groupID: 12}
	require.False(t, deepSeekToolsAccountAllowed(fetch, pat))
	require.False(t, deepSeekToolsAccountAllowed(fetch, nil))
	require.True(t, deepSeekToolsAccountAllowed(fetch, native))
	require.True(t, deepSeekToolsAccountAllowed(search, pat), "search may still use PAT citation results")
	require.True(t, deepSeekToolsAccountAllowed(nil, pat), "ordinary alpha retains PAT fallback")
}

func TestDeepSeekToolsFailoverExhaustedDoesNotExposeUpstreamBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/search", nil)
	failure := &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable,
		ResponseBody: []byte(`{"error":{"message":"FIXTURE_PRIVATE_ERROR"},"encrypted_output":"FIXTURE_CIPHERTEXT"}`)}
	bridge := &deepSeekToolsBridge{request: &deepseektools.Request{Operation: "search"}, groupID: 12}
	h := &OpenAIGatewayHandler{}
	h.handleAlphaSearchFailoverExhausted(c, failure, false, bridge)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.True(t, gjson.Valid(rec.Body.String()))
	require.NotEmpty(t, gjson.Get(rec.Body.String(), "error.message").String())
	require.NotContains(t, rec.Body.String(), "encrypted_output")
	require.NotContains(t, rec.Body.String(), "FIXTURE_CIPHERTEXT")
	require.NotContains(t, rec.Body.String(), "FIXTURE_PRIVATE_ERROR")
}
