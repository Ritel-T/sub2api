package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchGroupAdmissionKeepsPlatformBoundary(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite, service.PlatformDeepseek, service.PlatformGemini} {
		key := &service.APIKey{Group: &service.Group{Platform: platform}}
		require.Equal(t, platform == service.PlatformOpenAI || platform == service.PlatformComposite, alphaSearchGroupAllowed(key, nil))
		require.Equal(t, platform == service.PlatformDeepseek, alphaSearchGroupAllowed(key, &deepSeekToolsBridge{}))
	}
	require.False(t, alphaSearchGroupAllowed(nil, &deepSeekToolsBridge{}))
}

func TestDeepSeekToolRejectsInvalidRequestBeforeBackend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, platform, body string
		enabled              bool
		status               int
	}{
		{"wrong_platform", service.PlatformOpenAI, `{"query":"example"}`, true, 404},
		{"disabled", service.PlatformDeepseek, `{"query":"example"}`, false, 404},
		{"invalid_query", service.PlatformDeepseek, `{"query":" "}`, true, 400},
		{"invalid_type", service.PlatformDeepseek, `{"query":23}`, true, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/search", strings.NewReader(test.body))
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: test.platform}})
			cfg := &config.Config{}
			cfg.Gateway.DeepSeekTools = config.GatewayDeepSeekToolsConfig{Enabled: test.enabled, SearchGroupID: 12, SearchModel: "gpt-6-astra"}
			h := &OpenAIGatewayHandler{cfg: cfg}
			h.DeepSeekSearch(c)
			require.Equal(t, test.status, recorder.Code)
		})
	}
}
