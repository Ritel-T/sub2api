package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// CodexModels serves the Codex models manifest for Codex clients.
//
// Codex CLI and the Codex desktop app refresh their model picker from
// GET {base_url}/models?client_version=... (custom provider mode) or
// GET /backend-api/codex/models (chatgpt_base_url mode). Both routes land
// here. Pinned discovery takes precedence over local account model mappings;
// when disabled, groups with explicit mappings are generated locally;
// otherwise ChatGPT manifests are proxied verbatim and custom API key manifests
// receive provider-compatibility normalization plus short-lived caching.
func (h *OpenAIGatewayHandler) CodexModels(c *gin.Context) {
	if c.Request.Context().Err() != nil {
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "invalid_request_error", "API key group is required")
		return
	}
	if apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex models manifest is only available for OpenAI and Composite groups")
		return
	}
	if apiKey.ConcurrencyLimit > 0 {
		release, err := h.concurrencyHelper.AcquireAPIKeySlot(c.Request.Context(), apiKey.ID, apiKey.ConcurrencyLimit)
		if err != nil {
			h.handleConcurrencyError(c, err, "API key", false)
			return
		}
		defer release()
	}

	manifest, account, err := h.gatewayService.FetchGroupCodexModelsManifest(c.Request.Context(), apiKey.Group, c.Query("client_version"), modelsIfNoneMatch(c), h.maxAccountSwitches)
	if account != nil {
		setOpsSelectedAccount(c, account.ID, account.Platform)
	}
	if c.Request.Context().Err() != nil {
		return
	}
	if err != nil {
		errorType := "upstream_error"
		if infraerrors.Reason(err) == "api_error" {
			errorType = "api_error"
		}
		h.errorResponse(c, infraerrors.Code(err), errorType, infraerrors.Message(err))
		return
	}
	writeOpenAIModelsResponse(c, manifest)
}
