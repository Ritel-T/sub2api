package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ResolveSubscriptionBillingContext refreshes the charging route before any
// group-specific policy, account selection or forwarding. It never writes a
// response: protocol handlers retain their existing error envelopes.
func ResolveSubscriptionBillingContext(c *gin.Context, apiKeys *service.APIKeyService, subscriptions *service.SubscriptionService) (*service.APIKey, *service.UserSubscription, error) {
	key, ok := GetAPIKeyFromContext(c)
	if !ok || key == nil {
		return nil, nil, service.ErrAPIKeyNotFound
	}
	effective, sub, err := apiKeys.ResolveSubscriptionBilling(c.Request.Context(), key, subscriptions)
	if err != nil {
		return nil, nil, err
	}
	ApplySubscriptionBillingContext(c, effective, sub)
	return effective, sub, nil
}

func ApplySubscriptionBillingContext(c *gin.Context, effective *service.APIKey, sub *service.UserSubscription) {
	c.Set(string(ContextKeyAPIKey), effective)
	c.Set(string(ContextKeySubscription), sub)
	SetOpsFallbackAPIKey(c, effective)
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, effective.Group)
	ctx = service.WithSubscriptionOverflow(ctx, effective.SubscriptionOverflow)
	c.Request = c.Request.WithContext(ctx)
	service.SubscriptionBillingRouteStateFromContext(ctx).Set(effective)
}

func ResolveSubscriptionFallbackContext(c *gin.Context, apiKeys *service.APIKeyService, sub *service.UserSubscription, limitErr error) (*service.APIKey, *service.UserSubscription, error) {
	key, ok := GetAPIKeyFromContext(c)
	if !ok || key == nil {
		return nil, nil, service.ErrAPIKeyNotFound
	}
	effective, subscription, err := apiKeys.ResolveSubscriptionFallback(c.Request.Context(), key, sub, limitErr)
	if err != nil {
		return nil, nil, err
	}
	ApplySubscriptionBillingContext(c, effective, subscription)
	return effective, subscription, nil
}

func BeginSubscriptionBillingRouteResolution(c *gin.Context) func() {
	return service.SubscriptionBillingRouteStateFromContext(c.Request.Context()).Begin()
}
func FreezeSubscriptionBillingRoute(c *gin.Context) {
	key, _ := GetAPIKeyFromContext(c)
	service.SubscriptionBillingRouteStateFromContext(c.Request.Context()).Freeze(key)
}
