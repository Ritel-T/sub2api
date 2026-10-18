package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLiveOverflowRejectedBeforeAnyUpstreamDependencies(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/live", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{SubscriptionOverflow: &service.SubscriptionOverflowEvidence{SourceGroupID: 1, TargetGroupID: 2}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
	(&OpenAIGatewayHandler{}).Live(c)
	require.Equal(t, http.StatusForbidden, c.Writer.Status())
	require.Contains(t, rec.Body.String(), "subscription_fallback_unsupported")
}

func TestLiveOverflowAfterAdmissionRejectedBeforeCreatingSession(t *testing.T) {
	cache := &helperConcurrencyCacheStub{userSeq: []bool{true}}
	h := newServiceTierHandlerTest(t)
	h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
	sourceID, targetID := int64(1), int64(2)
	source := &service.APIKey{ID: 3, UserID: 7, GroupID: &sourceID, Group: &service.Group{ID: 1, Platform: service.PlatformOpenAI, AllowLive: true, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionFallbackGroupID: &targetID}, User: &service.User{ID: 7}}
	target := *source
	target.GroupID = &targetID
	target.Group = &service.Group{ID: 2, Platform: service.PlatformOpenAI, AllowLive: true}
	target.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroup: source.Group}
	router := gin.New()
	router.POST("/v1/live", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), source)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
		c.Request = c.Request.WithContext(service.WithSubscriptionBillingResolver(c.Request.Context(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
			require.Equal(t, 1, cache.userAcquireCalls, "subscription must refresh after admission")
			return &target, nil, nil
		}))
		h.Live(c)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(`{"sdp":"v=0\\r\\n","session":{"model":"gpt-live-test"}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "subscription_fallback_unsupported")
	require.Equal(t, 1, cache.userReleaseCalls)
	require.Zero(t, cache.accountAcquireCalls)
}

func TestGatewayCountTokensOverflowTargetRejectedBeforeBillingOrUpstream(t *testing.T) {
	sourceID, targetID := int64(1), int64(2)
	source := &service.APIKey{ID: 3, UserID: 7, GroupID: &sourceID, Group: &service.Group{ID: 1, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionFallbackGroupID: &targetID}, User: &service.User{ID: 7}}
	target := *source
	target.GroupID = &targetID
	target.Group = &service.Group{ID: 2, Platform: service.PlatformAnthropic, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"claude-allowed"}}}
	target.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroup: source.Group}
	// Both services are deliberately absent: model denial must precede them.
	h := &GatewayHandler{}
	router := gin.New()
	router.POST("/v1/messages/count_tokens", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), source)
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Request = c.Request.WithContext(service.WithSubscriptionBillingResolver(c.Request.Context(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
			return &target, nil, nil
		}))
		h.CountTokens(c)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-denied","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "not available")
}
