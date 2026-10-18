package middleware

import (
	"fmt"
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ValidateSubscriptionOverflowRequestPolicy applies both authorization scopes
// to the original HTTP request. It neither advances Gin nor commits a response.
func ValidateSubscriptionOverflowRequestPolicy(c *gin.Context, key *service.APIKey, body []byte) error {
	if key == nil {
		return nil
	}
	models := []string(nil)
	if model := groupModelAllowlistModelFromParams(c); model != "" {
		models = []string{model}
	} else {
		models = requestmodel.FromBodyCandidates(c.FullPath(), c.GetHeader("Content-Type"), body)
		if len(models) == 0 {
			if model := strings.TrimSpace(c.Query("model")); model != "" {
				models = []string{model}
			}
		}
	}
	permissions := service.APIKeyQueueRequestPermissionsFromContext(c.Request.Context())
	permissions.Models = models
	route := c.FullPath()
	if strings.HasSuffix(route, "/messages") && key.Group != nil && key.Group.Platform == service.PlatformOpenAI {
		permissions.Capabilities |= service.APIKeyQueueCapabilityMessagesDispatch
	}
	if strings.Contains(route, "/images/") || service.IsExplicitImageGenerationIntent(route, gjson.GetBytes(body, "model").String(), body) {
		permissions.Capabilities |= service.APIKeyQueueCapabilityImageGeneration
	}
	if strings.HasSuffix(route, "/live") {
		permissions.Capabilities |= service.APIKeyQueueCapabilityLive
	}
	if strings.HasSuffix(route, "/responses") || strings.HasSuffix(route, "/chat/completions") {
		permissions.Capabilities |= service.APIKeyQueueCapabilityForbiddenWhenClaudeCodeOnly
	}

	c.Request = c.Request.WithContext(service.WithAPIKeyQueueRequestPermissions(c.Request.Context(), permissions))
	groups := []*service.Group{key.Group}
	if key.SubscriptionOverflow != nil {
		groups = append([]*service.Group{key.SubscriptionOverflow.SourceGroup}, groups...)
	}
	for _, group := range groups {
		if group == nil {
			return infraerrors.Forbidden("SUBSCRIPTION_FALLBACK_INVALID", "Subscription overflow group is not available")
		}
		if group.ModelAllowlistEnabled() {
			for _, model := range models {
				if !group.ModelAllowlist.Allows(model) {
					MarkIngressRejected(c, IngressRejectModelNotAllowed)
					service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
					return infraerrors.NotFound("MODEL_NOT_ALLOWED", fmt.Sprintf("Model %q is not available for this group", model))
				}
			}
		}
		if group.StreamOnly && c.Request.Method == http.MethodPost {
			route := c.FullPath()
			if streamOnlyJSONRoutes[route] && !jsonBodyRequestsStream(body) {
				MarkIngressRejected(c, IngressRejectStreamRequired)
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
				return infraerrors.BadRequest("STREAM_REQUIRED", streamRequiredMessage)
			}
			if streamOnlyGeminiRoutes[route] {
				_, action, parsed := gemini.ParseModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
				if parsed && action == gemini.ActionGenerateContent {
					MarkIngressRejected(c, IngressRejectStreamRequired)
					service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
					return infraerrors.BadRequest("STREAM_REQUIRED", streamRequiredGeminiMessage)
				}
			}
		}
		projected := *key
		projected.Group = group
		if err := apiKeyQueueRevokedCapability(&projected, permissions); err != nil {
			return err
		}
	}
	if denied := service.FirstUserGroupDeniedModel(key.DeniedModelsInGroup(), models); denied != "" {
		MarkIngressRejected(c, IngressRejectModelNotAllowed)
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
		return infraerrors.NotFound("MODEL_NOT_ALLOWED", fmt.Sprintf("Model %q is not available for your account in this group", denied))
	}
	return nil
}
