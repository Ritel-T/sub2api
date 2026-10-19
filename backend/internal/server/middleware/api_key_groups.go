package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func isMultiGroupBillingInfoRequest(c *gin.Context, key *service.APIKey) bool {
	return c.Request.URL.Path == "/v1/sub2api/billing" && len(key.ConfiguredGroupIDs()) > 1
}

func selectAPIKeyRequestGroup(c *gin.Context, keys *service.APIKeyService, subscriptions *service.SubscriptionService, cfg *config.Config, key *service.APIKey, google bool) (*service.APIKey, error) {
	if len(key.ConfiguredGroupIDs()) < 2 || isMultiGroupBillingInfoRequest(c, key) || c.Request.URL.Path == "/v1/usage" || isAsyncImageTaskRead(c.Request.Method, c.Request.URL.Path) {
		return key, nil
	}
	var models []string
	var body []byte
	if model := groupModelAllowlistModelFromParams(c); model != "" {
		models = []string{model}
	}
	if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch {
		var ok bool
		body, ok = readAdmissionRequestBody(c)
		if !ok {
			return nil, nil
		}
		if len(models) == 0 {
			models = requestmodel.FromBodyCandidates(c.FullPath(), c.GetHeader("Content-Type"), body)
		}
	}
	if len(models) == 0 && c.Query("model") != "" {
		models = []string{c.Query("model")}
	}
	kind := "token"
	path := c.Request.URL.Path
	if strings.Contains(path, "/images/") {
		kind = "image"
	}
	if strings.Contains(path, "/videos") {
		kind = "video"
	}
	checkBilling := cfg.RunMode != config.RunModeSimple && c.Request.Method != http.MethodGet && !strings.HasSuffix(path, "/count_tokens")
	accepts := func(candidate *service.APIKey) bool {
		group := candidate.Group
		if checkBilling && !group.IsSubscriptionType() && apiKeyBalanceBelowAuthThreshold(candidate.User.Balance, cfg) {
			return false
		}
		if google && group.Platform != service.PlatformGemini && group.Platform != service.PlatformAntigravity && group.Platform != service.PlatformComposite {
			return false
		}
		if strings.HasPrefix(path, "/antigravity/") && group.Platform != service.PlatformAntigravity {
			return false
		}
		if kind == "image" && !service.GroupAllowsImageGeneration(group) {
			return false
		}
		if group.StreamOnly && c.Request.Method == http.MethodPost {
			if streamOnlyJSONRoutes[c.FullPath()] && !jsonBodyRequestsStream(body) {
				return false
			}
			if streamOnlyGeminiRoutes[c.FullPath()] && strings.HasSuffix(c.Param("modelAction"), ":generateContent") {
				return false
			}
		}
		if group.BlockSubagents && service.IsCodexSubagentRequest(c, body) {
			return false
		}
		if group.CodexCLIOnly {
			policy := service.CodexRestrictionPolicy{EngineFingerprintSignals: openai.DefaultEngineFingerprintSignals}
			if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
				policy.EngineFingerprintSignals = nil
			}
			if !service.DetectCodexGroupClient(c, group, policy, body).Matched {
				return false
			}
		}
		return true
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.UserID, key.UserID)
	selected, err := keys.SelectRequestGroup(ctx, key, models, kind, subscriptions, accepts, checkBilling)
	if err != nil {
		status := infraerrors.Code(err)
		message, reason := infraerrors.Message(err), infraerrors.Reason(err)
		if status >= 500 {
			message = "No selected group is currently available for this request"
		}
		if google {
			abortWithGoogleError(c, status, message)
		} else {
			AbortWithError(c, status, reason, message)
		}
		return nil, err
	}
	SetOpsFallbackAPIKey(c, selected)
	return selected, nil
}
