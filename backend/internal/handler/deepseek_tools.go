package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/deepseektools"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type deepSeekToolsBridge struct {
	request      *deepseektools.Request
	originalBody []byte
	groupID      int64
}

func (h *OpenAIGatewayHandler) DeepSeekSearch(c *gin.Context) { h.deepSeekTools(c, "search") }
func (h *OpenAIGatewayHandler) DeepSeekFetch(c *gin.Context)  { h.deepSeekTools(c, "fetch") }

func (h *OpenAIGatewayHandler) deepSeekTools(c *gin.Context, operation string) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || key.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if key.Group.Platform != service.PlatformDeepseek || h.cfg == nil || !h.cfg.Gateway.DeepSeekTools.Enabled {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Search tools are not enabled for this group")
		return
	}
	cfg := h.cfg.Gateway.DeepSeekTools
	if cfg.SearchGroupID <= 0 || strings.TrimSpace(cfg.SearchModel) == "" {
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Search backend is not configured")
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	request, err := deepseektools.ParseRequest(operation, body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	alpha, err := request.AlphaBody(cfg.SearchModel)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Invalid search request")
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(alpha))
	c.Request.ContentLength = int64(len(alpha))
	c.Header("X-Search-Provider", "openai")
	h.alphaSearch(c, &deepSeekToolsBridge{request: request, originalBody: body, groupID: cfg.SearchGroupID})
}

func alphaSearchGroupAllowed(key *service.APIKey, bridge *deepSeekToolsBridge) bool {
	if key == nil || key.Group == nil {
		return false
	}
	if bridge != nil {
		return key.Group.Platform == service.PlatformDeepseek
	}
	return key.Group.Platform == service.PlatformOpenAI || key.Group.Platform == service.PlatformComposite
}

func deepSeekToolsAccountAllowed(bridge *deepSeekToolsBridge, account *service.Account) bool {
	if bridge == nil || bridge.request.Operation != "fetch" {
		return true
	}
	return account != nil && !account.IsOpenAIPersonalAccessToken()
}

func (h *OpenAIGatewayHandler) handleAlphaSearchFailoverExhausted(c *gin.Context, failure *service.UpstreamFailoverError, started bool, bridge *deepSeekToolsBridge) {
	if bridge == nil {
		h.handleFailoverExhausted(c, failure, started)
		return
	}
	if started || c.Writer.Written() {
		return
	}
	h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Search backend is unavailable")
}
