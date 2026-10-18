//go:build unit

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type overflowAuthGroupRepo struct {
	service.GroupRepository
	target *service.Group
}

func (r *overflowAuthGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return r.target, nil
}

type overflowAuthUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *overflowAuthUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

type overflowAuthRateRepo struct {
	service.UserGroupRateRepository
}

func (*overflowAuthRateRepo) GetDeniedModelsByUserAndGroup(context.Context, int64, int64) ([]string, error) {
	return nil, nil
}
func (*overflowAuthRateRepo) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	return nil, nil
}
func (*overflowAuthRateRepo) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, nil
}

func TestSubscriptionOverflowAuthBothProtocolsKeepKeyIdentityAndWalletRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard", true: "google"}[google], func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			limit := 10.0
			targetID := int64(20)
			day := timezone.StartOfDay(time.Now())
			window := time.Now().Add(-time.Hour)
			source := &service.Group{ID: 10, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, SubscriptionFallbackGroupID: &targetID}
			user := &service.User{ID: 2, Status: service.StatusActive, Balance: 5}
			original := &service.APIKey{ID: 3, UserID: 2, Key: "test-only", Status: service.StatusActive, User: user, Group: source, GroupID: &source.ID}
			keyRepo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return original, nil }}
			groups := &overflowAuthGroupRepo{target: &service.Group{ID: 20, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}}
			keys := service.NewAPIKeyService(keyRepo, &overflowAuthUserRepo{user: user}, groups, nil, &overflowAuthRateRepo{}, nil, cfg)
			sub := &service.UserSubscription{ID: 4, UserID: 2, GroupID: 10, Status: service.SubscriptionStatusActive, StartsAt: time.Now().Add(-24 * time.Hour), ExpiresAt: time.Now().Add(24 * time.Hour), DailyUsageUSD: 10, DailyWindowStart: &day, WeeklyWindowStart: &window, MonthlyWindowStart: &window}
			subs := service.NewSubscriptionService(nil, &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
				copy := *sub
				return &copy, nil
			}}, nil, nil, cfg)
			defer subs.Stop()
			r := gin.New()
			if google {
				r.Use(APIKeyAuthWithSubscriptionGoogle(keys, subs, cfg))
			} else {
				r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(keys, subs, cfg)))
			}
			reached := false
			r.POST("/t", func(c *gin.Context) {
				reached = true
				got, _ := GetAPIKeyFromContext(c)
				require.Equal(t, original.ID, got.ID)
				require.Equal(t, int64(20), *got.GroupID)
				require.NotNil(t, got.SubscriptionOverflow)
				_, ok := GetSubscriptionFromContext(c)
				require.False(t, ok)
				require.Equal(t, int64(20), c.Request.Context().Value(ctxkey.Group).(*service.Group).ID)
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/t", nil)
			req.Header.Set("x-api-key", "test-only")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			require.True(t, reached)
			require.Equal(t, int64(10), *original.GroupID)
			require.Nil(t, original.SubscriptionOverflow)
		})
	}
}

func TestSubscriptionOverflowQueueRouteChangesOnlyInUnboundWindow(t *testing.T) {
	sourceID, targetID := int64(10), int64(20)
	source := &service.APIKey{ID: 3, UserID: 2, Status: service.StatusActive, User: &service.User{ID: 2, Status: service.StatusActive}, GroupID: &sourceID, Group: &service.Group{ID: sourceID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription}}
	target := *source
	target.GroupID = &targetID
	target.Group = &service.Group{ID: targetID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}
	ctx := service.WithSubscriptionBillingRouteState(context.Background(), source)
	ctx = service.WithSubscriptionBillingResolver(ctx, func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
		return &target, nil, nil
	})
	check := newAPIKeyAuthRevalidator(func(context.Context, string) (*service.APIKey, error) { return source, nil }, "test-only", "127.0.0.1", source)
	_, err := check(ctx)
	require.Error(t, err)
	cleanup := service.SubscriptionBillingRouteStateFromContext(ctx).Begin()
	_, err = check(ctx)
	require.NoError(t, err)
	service.SubscriptionBillingRouteStateFromContext(ctx).Freeze(&target)
	cleanup()
	_, err = check(ctx)
	require.NoError(t, err)
	ctx = service.WithSubscriptionBillingResolver(ctx, func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
		return source, nil, nil
	})
	_, err = check(ctx)
	require.Error(t, err, "recovered quota must reconnect rather than use the old account")
}

func TestSubscriptionOverflowQueueDoesNotTreatMissingSubscriptionAsOverflow(t *testing.T) {
	id := int64(10)
	key := &service.APIKey{ID: 3, UserID: 2, Status: service.StatusActive, User: &service.User{ID: 2, Status: service.StatusActive}, GroupID: &id, Group: &service.Group{ID: id, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription}}
	ctx := service.WithSubscriptionBillingResolver(context.Background(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
		return nil, nil, service.ErrSubscriptionNotFound
	})
	check := newAPIKeyAuthRevalidator(func(context.Context, string) (*service.APIKey, error) { return key, nil }, "test-only", "127.0.0.1", key)
	_, err := check(ctx)
	require.Error(t, err)
	require.True(t, errors.Is(err, service.ErrSubscriptionNotFound))
}

func TestSubscriptionOverflowQueueRejectsFreshSourceCapabilityRevocation(t *testing.T) {
	cases := []struct {
		name       string
		capability service.APIKeyQueueCapability
		revoke     func(*service.Group)
	}{
		{"messages_dispatch", service.APIKeyQueueCapabilityMessagesDispatch, func(g *service.Group) { g.AllowMessagesDispatch = false }},
		{"live", service.APIKeyQueueCapabilityLive, func(g *service.Group) { g.AllowLive = false }},
		{"image", service.APIKeyQueueCapabilityImageGeneration, func(g *service.Group) { g.AllowImageGeneration = false }},
		{"claude_code_only", service.APIKeyQueueCapabilityForbiddenWhenClaudeCodeOnly, func(g *service.Group) { g.ClaudeCodeOnly = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sourceID, targetID := int64(10), int64(20)
			source := &service.APIKey{ID: 3, UserID: 2, Status: service.StatusActive, User: &service.User{ID: 2, Status: service.StatusActive}, GroupID: &sourceID, Group: &service.Group{ID: sourceID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, AllowMessagesDispatch: true, AllowLive: true, AllowImageGeneration: true}}
			target := *source
			target.GroupID = &targetID
			target.Group = &service.Group{ID: targetID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, AllowMessagesDispatch: true, AllowLive: true, AllowImageGeneration: true}
			target.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroup: source.Group}
			ctx := service.WithSubscriptionBillingRouteState(context.Background(), &target)
			ctx = service.WithAPIKeyQueueRequestPermissions(ctx, service.APIKeyQueueRequestPermissions{Capabilities: tc.capability})
			ctx = service.WithSubscriptionBillingResolver(ctx, func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
				projected := target
				freshSource := *source.Group
				tc.revoke(&freshSource)
				evidence := *target.SubscriptionOverflow
				evidence.SourceGroup = &freshSource
				projected.SubscriptionOverflow = &evidence
				return &projected, nil, nil
			})
			check := newAPIKeyAuthRevalidator(func(context.Context, string) (*service.APIKey, error) { return source, nil }, "test-only", "127.0.0.1", source)
			_, err := check(ctx)
			require.Error(t, err, "a permissive target must not override a newly revoked source capability")
		})
	}
}
