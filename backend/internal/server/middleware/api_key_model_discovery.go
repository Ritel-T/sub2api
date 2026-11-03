package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const apiKeyModelDiscoveryGroupsKey = "api_key_model_discovery_groups"

// SetAPIKeyModelDiscoveryGroups stores a successfully resolved discovery list.
// Callers must preserve binding order and only install it for multi-group GET
// model discovery after authorization (or when constructing handler tests).
func SetAPIKeyModelDiscoveryGroups(c *gin.Context, groups []*service.APIKey) {
	c.Set(apiKeyModelDiscoveryGroupsKey, groups)
}

// GetAPIKeyModelDiscoveryGroups exposes resolved request-local key clones in
// binding order. True means authenticated multi-group model discovery only;
// single-group requests and security-penalty pins retain their normal path.
func GetAPIKeyModelDiscoveryGroups(c *gin.Context) ([]*service.APIKey, bool) {
	value, ok := c.Get(apiKeyModelDiscoveryGroupsKey)
	if !ok {
		return nil, false
	}
	groups, ok := value.([]*service.APIKey)
	return groups, ok
}

func isAPIKeyModelDiscoveryRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet {
		return false
	}
	path := c.Request.URL.Path
	switch path {
	case "/v1/models", "/models", "/backend-api/codex/models", "/v1beta/models",
		"/antigravity/models", "/antigravity/v1/models", "/antigravity/v1beta/models":
		return true
	}
	// Only the registered single-model retrieval routes qualify. In particular,
	// generation actions, image batch catalogues and arbitrary suffixes do not.
	for _, base := range []string{"/v1/models/", "/models/", "/v1beta/models/", "/antigravity/v1beta/models/"} {
		if model, ok := strings.CutPrefix(path, base); ok && model != "" && !strings.Contains(model, "/") {
			return true
		}
	}
	return false
}

// finishAPIKeyModelDiscoveryAuth bypasses group billing only after all candidate
// permissions resolved successfully. Catalogue reads still enforce key limits,
// even in simple mode, and never perform subscription maintenance or admission.
func finishAPIKeyModelDiscoveryAuth(c *gin.Context, key *service.APIKey, google bool) bool {
	if _, ok := GetAPIKeyModelDiscoveryGroups(c); !ok {
		return false
	}
	switch {
	case key.Status == service.StatusAPIKeyExpired || key.IsExpired():
		if google {
			abortWithGoogleError(c, http.StatusForbidden, "API key 已过期")
		} else {
			AbortWithError(c, http.StatusForbidden, "API_KEY_EXPIRED", "API key 已过期")
		}
	case key.Status == service.StatusAPIKeyQuotaExhausted || key.IsQuotaExhausted():
		if google {
			abortWithGoogleError(c, http.StatusTooManyRequests, "API key 额度已用完")
		} else {
			abortWithAPIKeyQuotaError(c)
		}
	default:
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.UserID, key.User.ID))
		c.Set(string(ContextKeyAPIKey), key)
		c.Set(string(ContextKeyUser), AuthSubject{UserID: key.User.ID, Concurrency: key.User.Concurrency})
		c.Set(string(ContextKeyUserRole), key.User.Role)
		setGroupContext(c, key.Group)
		c.Next()
	}
	return true
}
