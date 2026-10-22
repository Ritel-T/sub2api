//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGrokCustomVoiceHandlerRejectsAllObjectOperationsBeforeDependencies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"custom-voices", "custom-voices/shared123", "custom-voices/shared123/audio"} {
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodPatch, http.MethodDelete} {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(method, "/v1/"+endpoint, nil)
			c.Set("api_key", &service.APIKey{Group: &service.Group{Platform: service.PlatformGrok}})
			(&OpenAIGatewayHandler{}).GrokVoice(c, endpoint)
			require.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", method, endpoint, rec.Body.String())
			require.Contains(t, rec.Body.String(), "custom voices are unavailable")
		}
	}
}

func TestGrokTTSStreamHandlerRejectsCustomVoiceBeforeDependencies(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/tts?voice=eve&voice=shared123", nil)
	(&OpenAIGatewayHandler{}).GrokTTSStream(c)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
