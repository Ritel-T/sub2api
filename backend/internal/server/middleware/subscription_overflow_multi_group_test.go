//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type overflowMultiAuthGroupRepo struct {
	service.GroupRepository
	groups map[int64]*service.Group
}

func (r *overflowMultiAuthGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if g := r.groups[id]; g != nil {
		return g, nil
	}
	return nil, service.ErrGroupNotFound
}
func (r *overflowMultiAuthGroupRepo) GetByIDLite(ctx context.Context, id int64) (*service.Group, error) {
	return r.GetByID(ctx, id)
}

func TestSubscriptionOverflowMultiGroupAdmissionPinsSourceAndRejectsChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, change := range []string{"source removed", "source policy changed", "target Codex policy changed", "target subagent policy changed", "target effort cap changed", "target effort action changed", "target effort mapping changed", "quota recovered"} {
		t.Run(change, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			now := time.Now()
			limit, targetID := 10.0, int64(20)
			day, window := timezone.StartOfDay(now), now.Add(-time.Hour)
			primary := &service.Group{ID: 5, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
			source := &service.Group{ID: 10, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit, SubscriptionFallbackGroupID: &targetID}
			target := &service.Group{ID: targetID, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeStandard}
			user := &service.User{ID: 2, Status: service.StatusActive, Balance: 5}
			stored := &service.APIKey{ID: 3, UserID: user.ID, Key: "test-only", Status: service.StatusActive, User: user, Group: primary, GroupID: &primary.ID, GroupIDs: []int64{5, 10}, ConcurrencyLimit: 2}
			keyRepo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
				copyKey, copyUser := *stored, *stored.User
				copyKey.User = &copyUser
				return &copyKey, nil
			}}
			sub := &service.UserSubscription{ID: 4, UserID: user.ID, GroupID: source.ID, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(24 * time.Hour), DailyUsageUSD: 10, DailyWindowStart: &day, WeeklyWindowStart: &window, MonthlyWindowStart: &window}
			subRepo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) { cp := *sub; return &cp, nil }}
			groups := &overflowMultiAuthGroupRepo{groups: map[int64]*service.Group{5: primary, 10: source, 20: target}}
			keys := service.NewAPIKeyService(keyRepo, &overflowAuthUserRepo{user: user}, groups, subRepo, &overflowAuthRateRepo{}, nil, cfg)
			subs := service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
			defer subs.Stop()
			selected, err := keys.PinAPIKeyGroup(context.Background(), stored, source.ID)
			require.NoError(t, err)
			effective, _, err := keys.ResolveSubscriptionFallback(context.Background(), selected, sub, service.ErrDailyLimitExceeded)
			require.NoError(t, err)
			require.NotContains(t, effective.GroupIDs, targetID, "wallet target is intentionally not an explicit Key group")
			router := gin.New()
			router.Use(func(c *gin.Context) {
				nextWithAPIKeyAdmissionOwner(c, keys, stored.Key, "127.0.0.1", effective, false, subs)
			})
			router.POST("/turn", func(c *gin.Context) {
				concurrency := service.NewConcurrencyService(nil)
				limit, installed, err := concurrency.RevalidateAPIKeyQueueTurn(c.Request.Context())
				require.NoError(t, err, "pin original subscription source rather than the unbound wallet target")
				require.True(t, installed)
				require.Equal(t, 2, limit)
				switch change {
				case "source removed":
					stored.GroupIDs = []int64{5}
				case "source policy changed":
					source.BlockSubagents = true
				case "target Codex policy changed":
					target.CodexCLIOnly = true
				case "target subagent policy changed":
					target.BlockSubagents = true
				case "target effort cap changed":
					target.MaxReasoningEffort = "max"
				case "target effort action changed":
					target.MaxReasoningEffortOverLimit = "reject"
				case "target effort mapping changed":
					target.ReasoningEffortMappings = []service.ReasoningEffortMapping{{From: "ultra", To: "max"}}
				case "quota recovered":
					sub.DailyUsageUSD = 0
				}
				_, _, err = concurrency.RevalidateAPIKeyQueueTurn(c.Request.Context())
				require.Error(t, err, "bound request must reject changed authorization or billing before another send")
				c.Status(http.StatusNoContent)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/turn", nil))
			require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
			require.Equal(t, primary.ID, *stored.GroupID)
		})
	}
}
