package handler

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type subscriptionOverflowAuditError struct{ decision *securityaudit.Decision }

func (e *subscriptionOverflowAuditError) Error() string { return securityAuditMessage(e.decision) }

type subscriptionBillingCheck func(context.Context, *service.APIKey, *service.UserSubscription) error

// The only place an HTTP handler may change its charging route is after user
// admission and before group policies/account selection. The ordinary billing
// check still runs exactly once for a successful route (quota failure occurs
// before RPM increments). No completed upstream operation is retried here.
func resolveRequestSubscriptionBilling(c *gin.Context, keys *service.APIKeyService, cache *service.BillingCacheService, body []byte, check subscriptionBillingCheck, audit func(*service.APIKey) *securityaudit.Decision) (*service.APIKey, *service.UserSubscription, error) {
	key, _ := middleware2.GetAPIKeyFromContext(c)
	sub, _ := middleware2.GetSubscriptionFromContext(c)
	if key == nil {
		return nil, nil, service.ErrAPIKeyNotFound
	}
	routeResolved := key.SubscriptionOverflow != nil || (key.Group != nil && key.Group.IsSubscriptionType() && key.Group.SubscriptionFallbackGroupID != nil)
	if routeResolved {
		var err error
		key, sub, err = middleware2.ResolveSubscriptionBillingContext(c, keys, nil)
		if err != nil {
			return nil, nil, err
		}
	}
	prepare := func() error {
		if routeResolved {
			if err := middleware2.ValidateSubscriptionOverflowRequestPolicy(c, key, body); err != nil {
				return err
			}
		}
		if key.SubscriptionOverflow != nil {
			if decision := audit(key); decision != nil && !decision.AllowNextStage {
				return &subscriptionOverflowAuditError{decision}
			}
		}
		return nil
	}
	if err := prepare(); err != nil {
		return nil, nil, err
	}
	err := check(c.Request.Context(), key, sub)
	// The billing cache can observe a concurrently completed charge before the
	// subscription repository does. Resolve that quota failure before any policy
	// captures a group or an account is selected.
	if service.IsSubscriptionQuotaError(err) && key.Group != nil && key.Group.SubscriptionFallbackGroupID != nil && sub != nil {
		if cache == nil {
			return nil, nil, service.ErrBillingServiceUnavailable
		}
		latest, loadErr := cache.GetSubscriptionStatus(c.Request.Context(), key.UserID, key.Group.ID)
		if loadErr != nil || latest == nil {
			return nil, nil, service.ErrBillingServiceUnavailable
		}
		copySub := *sub
		copySub.DailyUsageUSD, copySub.WeeklyUsageUSD, copySub.MonthlyUsageUSD = latest.DailyUsage, latest.WeeklyUsage, latest.MonthlyUsage
		key, sub, err = middleware2.ResolveSubscriptionFallbackContext(c, keys, &copySub, err)
		if err != nil {
			return nil, nil, err
		}
		if err = prepare(); err != nil {
			return nil, nil, err
		}
		err = check(c.Request.Context(), key, sub)
	}
	if err != nil {
		return nil, nil, err
	}
	middleware2.FreezeSubscriptionBillingRoute(c)
	return key, sub, nil
}

func overflowAuditProtocol(c *gin.Context) string {
	route := c.FullPath()
	switch {
	case strings.HasSuffix(route, "/messages"):
		return service.ContentModerationProtocolAnthropicMessages
	case strings.HasSuffix(route, "/chat/completions"):
		return service.ContentModerationProtocolOpenAIChat
	case strings.Contains(route, "/images/"):
		return service.ContentModerationProtocolOpenAIImages
	case strings.Contains(route, "/v1beta/"):
		return service.ContentModerationProtocolGemini
	default:
		return service.ContentModerationProtocolOpenAIResponses
	}
}

func (h *OpenAIGatewayHandler) resolveSubscriptionBilling(c *gin.Context, body []byte) (*service.APIKey, *service.UserSubscription, error) {
	return resolveRequestSubscriptionBilling(c, h.apiKeyService, h.billingCacheService, body, h.checkRegionalBillingEligibility, func(key *service.APIKey) *securityaudit.Decision {
		subject, _ := middleware2.GetAuthSubjectFromContext(c)
		return h.checkSecurityAudit(c, requestLogger(c, "handler.subscription_overflow"), key, subject, overflowAuditProtocol(c), subscriptionOverflowRequestModel(c, body), body)
	})
}

func (h *GatewayHandler) resolveSubscriptionBilling(c *gin.Context, body []byte) (*service.APIKey, *service.UserSubscription, error) {
	return resolveRequestSubscriptionBilling(c, h.apiKeyService, h.billingCacheService, body, func(ctx context.Context, key *service.APIKey, sub *service.UserSubscription) error {
		return h.billingCacheService.CheckBillingEligibility(ctx, key.User, key, key.Group, sub, service.QuotaPlatform(ctx, key))
	}, func(key *service.APIKey) *securityaudit.Decision {
		subject, _ := middleware2.GetAuthSubjectFromContext(c)
		return h.checkSecurityAudit(c, requestLogger(c, "handler.subscription_overflow"), key, subject, overflowAuditProtocol(c), subscriptionOverflowRequestModel(c, body), body)
	})
}

func subscriptionBillingErrorDetails(err error) (int, string, string, int) {
	if audit, ok := err.(*subscriptionOverflowAuditError); ok {
		return securityAuditStatus(audit.decision), securityAuditErrorCode(audit.decision), securityAuditMessage(audit.decision), 0
	}
	reason := infraerrors.Reason(err)
	switch reason {
	case "MODEL_NOT_ALLOWED", "STREAM_REQUIRED", "SUBSCRIPTION_FALLBACK_INVALID", "SUBSCRIPTION_FALLBACK_NOT_ALLOWED", "API_KEY_GROUP_CHANGED", "CLAUDE_CODE_ONLY", "MESSAGES_DISPATCH_NOT_ALLOWED", "LIVE_NOT_ALLOWED", "IMAGE_GENERATION_NOT_ALLOWED":
		return infraerrors.Code(err), strings.ToLower(reason), infraerrors.Message(err), 0
	}
	return billingErrorDetails(err)
}

func subscriptionOverflowRequestModel(c *gin.Context, body []byte) string {
	if model := strings.TrimSpace(c.Param("modelAction")); model != "" {
		model = strings.TrimPrefix(model, "/")
		if i := strings.LastIndex(model, ":"); i >= 0 {
			model = model[:i]
		}
		return model
	}
	models := requestmodel.FromBodyCandidates(c.FullPath(), c.GetHeader("Content-Type"), body)
	if len(models) > 0 {
		return models[0]
	}
	return strings.TrimSpace(c.Query("model"))
}
