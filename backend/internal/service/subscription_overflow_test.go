//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type overflowGroupRepo struct {
	GroupRepository
	target *Group
	err    error
	calls  int
}

func (r *overflowGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	r.calls++
	return r.target, r.err
}

type overflowUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *overflowUserRepo) GetByID(context.Context, int64) (*User, error) { return r.user, r.err }

type overflowRateRepo struct {
	UserGroupRateRepository
	sourceID   int64
	err        error
	multiplier float64
}

func (r *overflowRateRepo) GetDeniedModelsByUserAndGroup(_ context.Context, _ int64, group int64) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	if group == r.sourceID {
		return []string{"source-denied"}, nil
	}
	return []string{"target-denied"}, nil
}
func (r *overflowRateRepo) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	n := 17
	return &n, r.err
}
func (r *overflowRateRepo) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	return &r.multiplier, r.err
}

type overflowSubRepo struct {
	UserSubscriptionRepository
	sub   *UserSubscription
	err   error
	calls int
}

func (r *overflowSubRepo) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	r.calls++
	if r.sub == nil {
		return nil, r.err
	}
	cp := *r.sub
	return &cp, r.err
}

type overflowKeyRepo struct {
	APIKeyRepository
	key *APIKey
}

func (r *overflowKeyRepo) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	cp := *r.key
	user := *r.key.User
	cp.User = &user
	return &cp, nil
}

func overflowFixture(t *testing.T) (*APIKeyService, *APIKey, *UserSubscription, *overflowGroupRepo, *overflowSubRepo) {
	t.Helper()
	now := time.Now()
	day := timezone.StartOfDay(now)
	window := now.Add(-time.Hour)
	limit := 10.0
	targetID := int64(20)
	source := &Group{ID: 10, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit, SubscriptionFallbackGroupID: &targetID}
	user := &User{ID: 2, Status: StatusActive, Balance: 8, UserGroupDeniedModels: []string{"source-denied"}}
	key := &APIKey{ID: 3, UserID: 2, Key: "test-only", GroupID: &source.ID, Group: source, User: user, Status: StatusActive, Quota: 100, RateLimit5h: 10, ConcurrencyLimit: 2, IPWhitelist: []string{"127.0.0.1"}}
	sub := &UserSubscription{ID: 4, UserID: 2, GroupID: 10, Status: SubscriptionStatusActive, StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), DailyWindowStart: &day, WeeklyWindowStart: &window, MonthlyWindowStart: &window, DailyUsageUSD: 10}
	gr := &overflowGroupRepo{target: &Group{ID: 20, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 2}}
	sr := &overflowSubRepo{sub: sub}
	svc := &APIKeyService{groupRepo: gr, userRepo: &overflowUserRepo{user: user}, userGroupRateRepo: &overflowRateRepo{sourceID: 10, multiplier: 1.25}, apiKeyRepo: &overflowKeyRepo{key: key}, cfg: &config.Config{RunMode: config.RunModeStandard}}
	return svc, key, sub, gr, sr
}

func TestSubscriptionOverflowOnlyRecurringQuotaErrors(t *testing.T) {
	for _, e := range []error{ErrDailyLimitExceeded, ErrWeeklyLimitExceeded, ErrMonthlyLimitExceeded} {
		t.Run(e.Error(), func(t *testing.T) {
			svc, key, sub, _, _ := overflowFixture(t)
			got, subscription, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, e)
			require.NoError(t, err)
			require.Nil(t, subscription)
			require.Equal(t, int64(20), *got.GroupID)
			require.NotNil(t, got.SubscriptionOverflow)
		})
	}
	for _, e := range []error{ErrSubscriptionExpired, ErrSubscriptionNotFound, ErrSubscriptionSuspended, ErrAPIKeyQuotaExhausted, ErrAPIKeyRateLimit5hExceeded, ErrBillingServiceUnavailable, errors.New("database unavailable")} {
		t.Run(e.Error(), func(t *testing.T) {
			svc, key, sub, gr, _ := overflowFixture(t)
			_, _, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, e)
			require.ErrorIs(t, err, e)
			require.Zero(t, gr.calls)
		})
	}
}

func TestSubscriptionOverflowClonesKeyAndUserPoliciesAndUsesTargetBilling(t *testing.T) {
	svc, key, sub, _, _ := overflowFixture(t)
	got, subscription, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, ErrDailyLimitExceeded)
	require.NoError(t, err)
	require.Nil(t, subscription)
	require.NotSame(t, key, got)
	require.NotSame(t, key.User, got.User)
	require.Equal(t, int64(10), *key.GroupID)
	require.Nil(t, key.SubscriptionOverflow)
	require.Nil(t, key.User.UserGroupRPMOverride)
	require.Equal(t, []string{"source-denied"}, key.DeniedModelsInGroup())
	require.ElementsMatch(t, []string{"source-denied", "target-denied"}, got.DeniedModelsInGroup())
	require.Equal(t, 17, *got.User.UserGroupRPMOverride)
	require.Equal(t, key.ID, got.ID)
	require.Equal(t, key.Quota, got.Quota)
	require.Equal(t, key.RateLimit5h, got.RateLimit5h)
	require.Equal(t, key.ConcurrencyLimit, got.ConcurrencyLimit)
	require.Equal(t, key.IPWhitelist, got.IPWhitelist)
	require.Equal(t, key.Group.ID, SubscriptionOverflowSourceAPIKey(got).Group.ID)
	gateway := &GatewayService{userGroupRateRepo: svc.userGroupRateRepo}
	multiplier, _, _ := gateway.recordUsageMultipliers(context.Background(), got, got.User, time.Now())
	require.Equal(t, 1.25, multiplier)
	cmd := buildUsageBillingCommand("overflow", &UsageLog{RateMultiplier: multiplier}, &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 2, ActualCost: 2 * multiplier}, User: got.User, APIKey: got, Account: &Account{ID: 5}, APIKeyService: apiKeyQuotaUpdaterStub{}})
	require.Equal(t, 2.5, cmd.BalanceCost)
	require.Zero(t, cmd.SubscriptionCost)
	require.Nil(t, cmd.SubscriptionID)
	require.Equal(t, 2.5, cmd.APIKeyQuotaCost)
	require.Equal(t, 2.5, cmd.APIKeyRateLimitCost)
	require.NotNil(t, cmd.SubscriptionOverflow)
	require.True(t, cmd.SubscriptionOverflow.Valid())
	require.Equal(t, 1.25, cmd.SubscriptionOverflow.RateMultiplier)
	require.Equal(t, "daily", cmd.SubscriptionOverflow.Dimension)
	require.Equal(t, *sub.DailyWindowStart, cmd.SubscriptionOverflow.WindowStart)
}

func TestSubscriptionOverflowRejectsUnsafeTargetsAndAuthorization(t *testing.T) {
	tests := []struct {
		name   string
		change func(*APIKeyService, *APIKey, *UserSubscription, *overflowGroupRepo)
	}{
		{"disabled", func(_ *APIKeyService, _ *APIKey, _ *UserSubscription, g *overflowGroupRepo) {
			g.target.Status = "disabled"
		}},
		{"subscription target", func(_ *APIKeyService, _ *APIKey, _ *UserSubscription, g *overflowGroupRepo) {
			g.target.SubscriptionType = SubscriptionTypeSubscription
		}},
		{"other platform", func(_ *APIKeyService, _ *APIKey, _ *UserSubscription, g *overflowGroupRepo) {
			g.target.Platform = PlatformGemini
		}},
		{"composite", func(_ *APIKeyService, k *APIKey, _ *UserSubscription, g *overflowGroupRepo) {
			k.Group.Platform = PlatformComposite
			g.target.Platform = PlatformComposite
		}},
		{"unauthorized exclusive", func(_ *APIKeyService, _ *APIKey, _ *UserSubscription, g *overflowGroupRepo) {
			g.target.IsExclusive = true
		}},
		{"restricted public", func(_ *APIKeyService, k *APIKey, _ *UserSubscription, _ *overflowGroupRepo) {
			k.User.RestrictPublicGroups = true
		}},
		{"balance exhausted", func(_ *APIKeyService, k *APIKey, _ *UserSubscription, _ *overflowGroupRepo) { k.User.Balance = 0 }},
		{"expired subscription", func(_ *APIKeyService, _ *APIKey, s *UserSubscription, _ *overflowGroupRepo) {
			s.ExpiresAt = time.Now().Add(-time.Hour)
		}},
		{"missing user repo", func(s *APIKeyService, _ *APIKey, _ *UserSubscription, _ *overflowGroupRepo) { s.userRepo = nil }},
		{"missing policy repo", func(s *APIKeyService, _ *APIKey, _ *UserSubscription, _ *overflowGroupRepo) {
			s.userGroupRateRepo = nil
		}},
		{"policy lookup failure", func(s *APIKeyService, _ *APIKey, _ *UserSubscription, _ *overflowGroupRepo) {
			s.userGroupRateRepo = &overflowRateRepo{err: errors.New("db failed")}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, key, sub, g, _ := overflowFixture(t)
			tt.change(svc, key, sub, g)
			_, _, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, ErrDailyLimitExceeded)
			require.Error(t, err)
			require.Equal(t, int64(10), *key.GroupID)
		})
	}
}

func TestSubscriptionOverflowFreshReadAndAutomaticReturn(t *testing.T) {
	svc, key, sub, _, repo := overflowFixture(t)
	subs := &SubscriptionService{userSubRepo: repo, now: time.Now}
	// A stale auth-facing snapshot can say zero while the database quota is spent.
	stale := *sub
	stale.DailyUsageUSD = 0
	require.Zero(t, stale.DailyUsageUSD)
	effective, active, err := svc.ResolveSubscriptionBilling(context.Background(), key, subs)
	require.NoError(t, err)
	require.Nil(t, active)
	require.NotNil(t, effective.SubscriptionOverflow)
	require.Equal(t, 1, repo.calls)
	repo.sub.DailyUsageUSD = 0
	restored, active, err := svc.ResolveSubscriptionBilling(context.Background(), effective, subs)
	require.NoError(t, err)
	require.NotNil(t, active)
	require.Equal(t, int64(10), *restored.GroupID)
	require.Nil(t, restored.SubscriptionOverflow)
	require.Equal(t, 2, repo.calls)
}

func TestSubscriptionOverflowResolverNeverFallsBackOnMissingSubscriptionOrStoreFailure(t *testing.T) {
	for _, e := range []error{ErrSubscriptionNotFound, ErrSubscriptionExpired, errors.New("db failed")} {
		svc, key, _, groups, repo := overflowFixture(t)
		repo.sub = nil
		repo.err = e
		_, _, err := svc.ResolveSubscriptionBilling(context.Background(), key, &SubscriptionService{userSubRepo: repo, now: time.Now})
		require.ErrorIs(t, err, e)
		require.Zero(t, groups.calls)
	}
}

func TestSubscriptionOverflowNoNotificationForFreeOrSubscriptionBilling(t *testing.T) {
	svc, key, sub, _, _ := overflowFixture(t)
	effective, _, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, ErrDailyLimitExceeded)
	require.NoError(t, err)
	for _, tc := range []struct {
		actual       float64
		subscription bool
	}{{0, false}, {2, true}} {
		p := &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 2, ActualCost: tc.actual}, APIKey: effective, User: effective.User, Account: &Account{ID: 5}, Subscription: sub, IsSubscriptionBill: tc.subscription}
		cmd := buildUsageBillingCommand("noop-notify", nil, p)
		require.Nil(t, cmd.SubscriptionOverflow)
	}
}

func TestSubscriptionOverflowNeverUsesNonAtomicLegacyBilling(t *testing.T) {
	svc, key, sub, _, _ := overflowFixture(t)
	effective, _, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, ErrDailyLimitExceeded)
	require.NoError(t, err)
	p := &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 2, ActualCost: 2}, APIKey: effective, User: effective.User, Account: &Account{ID: 5}}
	applied, err := applyUsageBilling(context.Background(), "overflow", nil, p, &billingDeps{}, nil)
	require.False(t, applied)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
	applied, err = applyUsageBilling(context.Background(), "", nil, p, &billingDeps{}, &simpleModeUsageBillingRepoStub{})
	require.False(t, applied)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestSubscriptionOverflowBillingRecheckDoesNotConsumeRPMButStillGuardsBalance(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 5}
	rpm := &userRPMCacheStub{userGroupCounts: []int{1, 2}}
	svc := NewBillingCacheService(cache, nil, nil, nil, rpm, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	user := &User{ID: 2}
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeStandard, RPMLimit: 1}
	ctx := context.Background()
	require.NoError(t, svc.CheckBillingEligibility(ctx, user, nil, group, nil, ""))
	require.NoError(t, svc.CheckBillingEligibilityWithoutRPM(ctx, user, nil, group, nil, ""))
	require.NoError(t, svc.CheckBillingEligibilityWithoutRPM(ctx, user, nil, group, nil, ""))
	require.EqualValues(t, 1, rpm.userGroupCalls)
	require.ErrorIs(t, svc.CheckBillingEligibility(ctx, user, nil, group, nil, ""), ErrGroupRPMExceeded)
	require.EqualValues(t, 2, rpm.userGroupCalls, "the next semantic turn must consume its own RPM")
	cache.balance = 0
	require.ErrorIs(t, svc.CheckBillingEligibilityWithoutRPM(ctx, user, nil, group, nil, ""), ErrInsufficientBalance)
	require.EqualValues(t, 2, rpm.userGroupCalls)
}

func TestSubscriptionOverflowResolverUsesFreshSourceCancellationAndAuthorization(t *testing.T) {
	t.Run("cancelled fallback preserves legacy exact allowance", func(t *testing.T) {
		svc, key, sub, groups, repo := overflowFixture(t)
		latest := *key
		group := *key.Group
		group.SubscriptionFallbackGroupID = nil
		latest.Group = &group
		svc.apiKeyRepo = &overflowKeyRepo{key: &latest}
		effective, active, err := svc.ResolveSubscriptionBilling(context.Background(), key, &SubscriptionService{userSubRepo: repo, now: time.Now})
		require.NoError(t, err)
		require.NotNil(t, active)
		require.Nil(t, effective.SubscriptionOverflow)
		require.Equal(t, int64(10), *effective.GroupID)
		require.Zero(t, groups.calls)
		require.Equal(t, 10.0, sub.DailyUsageUSD)
	})
	t.Run("active private subscription authorizes source without allowed groups", func(t *testing.T) {
		svc, key, _, _, repo := overflowFixture(t)
		key.Group.IsExclusive = true
		key.User.RestrictPublicGroups = true
		key.User.AllowedGroups = []int64{20}
		effective, active, err := svc.ResolveSubscriptionBilling(context.Background(), key, &SubscriptionService{userSubRepo: repo, now: time.Now})
		require.NoError(t, err)
		require.Nil(t, active)
		require.NotNil(t, effective.SubscriptionOverflow)
		require.Equal(t, int64(20), *effective.GroupID)
	})
}

func TestSubscriptionOverflowExactQuotaBoundaryForEveryWindow(t *testing.T) {
	now := time.Now()
	day := timezone.StartOfDay(now)
	window := now.Add(-time.Hour)
	svc := &SubscriptionService{now: func() time.Time { return now }}
	for _, dimension := range []string{"daily", "weekly", "monthly"} {
		for _, overflow := range []bool{false, true} {
			for _, usage := range []float64{9.99, 10, 10.01} {
				t.Run(fmt.Sprintf("%s/overflow=%t/usage=%g", dimension, overflow, usage), func(t *testing.T) {
					limit, targetID := 10.0, int64(20)
					group := &Group{}
					sub := &UserSubscription{Status: SubscriptionStatusActive, StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), DailyWindowStart: &day, WeeklyWindowStart: &window, MonthlyWindowStart: &window}
					var expected error
					switch dimension {
					case "daily":
						group.DailyLimitUSD = &limit
						sub.DailyUsageUSD = usage
						expected = ErrDailyLimitExceeded
					case "weekly":
						group.WeeklyLimitUSD = &limit
						sub.WeeklyUsageUSD = usage
						expected = ErrWeeklyLimitExceeded
					case "monthly":
						group.MonthlyLimitUSD = &limit
						sub.MonthlyUsageUSD = usage
						expected = ErrMonthlyLimitExceeded
					}
					if overflow {
						group.SubscriptionFallbackGroupID = &targetID
					}
					maintenance, err := svc.ValidateAndCheckLimits(sub, group)
					require.False(t, maintenance)
					if usage > limit || overflow && usage == limit {
						require.ErrorIs(t, err, expected)
					} else {
						require.NoError(t, err)
					}
				})
			}
		}
	}
}

func TestSubscriptionOverflowSnapshotsNegotiatedRateAndRejectsInvalidRates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rate  float64
		err   error
		allow bool
	}{
		{"custom", 0.42, nil, true},
		{"free", 0, nil, true},
		{"negative", -0.1, nil, false},
		{"nan", math.NaN(), nil, false},
		{"infinite", math.Inf(1), nil, false},
		{"repository unavailable", 0.42, errors.New("rate unavailable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, key, sub, _, _ := overflowFixture(t)
			rates := svc.userGroupRateRepo.(*overflowRateRepo)
			rates.multiplier, rates.err = tc.rate, tc.err
			effective, _, err := svc.ResolveSubscriptionFallback(context.Background(), key, sub, ErrDailyLimitExceeded)
			if !tc.allow {
				require.ErrorIs(t, err, ErrBillingServiceUnavailable)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, effective.SubscriptionOverflow.BaseRateMultiplier)
			require.Equal(t, tc.rate, *effective.SubscriptionOverflow.BaseRateMultiplier)
			rates.err = errors.New("repository failed after admission")
			rates.multiplier = 0.6
			ctx := WithSubscriptionOverflow(context.Background(), effective.SubscriptionOverflow)
			gateway := &GatewayService{userGroupRateRepo: rates}
			openai := &OpenAIGatewayService{}
			require.Equal(t, tc.rate, gateway.ResolveUserGroupRateMultiplier(ctx, effective.UserID, *effective.GroupID, 0.6))
			require.Equal(t, tc.rate, openai.ResolveUserGroupRateMultiplier(ctx, effective.UserID, *effective.GroupID, 0.6))
			multiplier, _, _ := gateway.recordUsageMultipliers(context.Background(), effective, effective.User, time.Now())
			require.Equal(t, tc.rate, multiplier, "detached workers must retain the accepted price")
			text, image := (inflightEstimateDeps{userGroupRate: func(context.Context, int64, int64, float64) float64 {
				t.Fatal("overflow estimate must not reload negotiated rate")
				return 0.6
			}}).rates(context.Background(), effective)
			require.Equal(t, tc.rate, text)
			require.Equal(t, tc.rate, image)
			accountMultiplier := 2.0
			adjusted, _, _ := gateway.recordUsageMultipliers(context.Background(), effective, effective.User, time.Now(), &Account{GroupRateMultiplier: &accountMultiplier})
			require.Equal(t, 2*tc.rate, adjusted, "account adjustment still applies after the base snapshot")
			snapshot, ok := subscriptionOverflowBaseRateFromKey(effective)
			require.True(t, ok)
			require.Equal(t, tc.rate, snapshot)
		})
	}
}
