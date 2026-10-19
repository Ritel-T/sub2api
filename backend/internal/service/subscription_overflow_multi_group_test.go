//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type overflowMultiGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *overflowMultiGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if g := r.groups[id]; g != nil {
		return g, nil
	}
	return nil, ErrGroupNotFound
}
func (r *overflowMultiGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return r.GetByID(ctx, id)
}

func overflowMultiGroupFixture(t *testing.T) (*APIKeyService, *APIKey, *SubscriptionService, *overflowSubRepo, *overflowMultiGroupRepo) {
	t.Helper()
	svc, source, _, oldGroups, repo := overflowFixture(t)
	primary := &Group{ID: 5, Status: StatusActive, Hydrated: true, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 3}
	stored := *source
	stored.GroupID, stored.Group = &primary.ID, primary
	stored.GroupIDs = []int64{primary.ID, source.Group.ID}
	source.GroupIDs = append([]int64(nil), stored.GroupIDs...)
	groups := &overflowMultiGroupRepo{groups: map[int64]*Group{primary.ID: primary, source.Group.ID: source.Group, oldGroups.target.ID: oldGroups.target}}
	svc.groupRepo, svc.userSubRepo = groups, repo
	svc.apiKeyRepo = &overflowKeyRepo{key: &stored}
	svc.groupRouter = multiGroupRouterStub{oldGroups.target.ID: {Available: true}}
	subs := &SubscriptionService{userSubRepo: repo, now: time.Now}
	return svc, &stored, subs, repo, groups
}

func TestSubscriptionOverflowMultiGroupSelectionAndQuotaRecovery(t *testing.T) {
	svc, stored, subs, repo, _ := overflowMultiGroupFixture(t)
	selected, err := svc.SelectRequestGroup(context.Background(), stored, []string{"gpt-5"}, "token", subs, nil, true)
	require.NoError(t, err)
	require.Equal(t, int64(10), *selected.GroupID, "return the subscription source even when only its wallet route has capacity")
	require.Nil(t, selected.SubscriptionOverflow, "normal auth owns the final charging projection")
	require.Equal(t, int64(5), *stored.GroupID)
	effective, active, err := svc.ResolveSubscriptionBilling(context.Background(), selected, subs)
	require.NoError(t, err)
	require.Nil(t, active)
	require.Equal(t, int64(20), *effective.GroupID)
	require.Equal(t, int64(10), *SubscriptionOverflowSourceAPIKey(effective).GroupID)
	require.ElementsMatch(t, []string{"source-denied", "target-denied"}, effective.DeniedModelsInGroup())
	repo.sub.DailyUsageUSD = 0
	restored, active, err := svc.ResolveSubscriptionBilling(context.Background(), effective, subs)
	require.NoError(t, err)
	require.NotNil(t, active)
	require.Equal(t, int64(10), *restored.GroupID)
	require.Nil(t, restored.SubscriptionOverflow)
	require.Equal(t, int64(5), *stored.GroupID, "never rewrite the persisted primary")
}

func TestSubscriptionOverflowMultiGroupUsesWalletPriceAndBothPolicies(t *testing.T) {
	t.Run("wallet price", func(t *testing.T) {
		svc, stored, subs, _, _ := overflowMultiGroupFixture(t)
		svc.userGroupRateRepo = multiGroupRateStub{rates: map[int64]float64{5: 0.8, 10: 0.1, 20: 1.25}}
		svc.groupRouter = multiGroupRouterStub{5: {Available: true}, 20: {Available: true}}
		selected, err := svc.SelectRequestGroup(context.Background(), stored, []string{"gpt-5"}, "token", subs, nil, true)
		require.NoError(t, err)
		require.Equal(t, int64(5), *selected.GroupID, "exhausted subscription's low multiplier cannot conceal its higher wallet price")
	})
	for _, model := range []string{"source-denied", "target-denied"} {
		t.Run(model, func(t *testing.T) {
			svc, stored, subs, _, _ := overflowMultiGroupFixture(t)
			_, err := svc.SelectRequestGroup(context.Background(), stored, []string{model}, "token", subs, nil, true)
			require.Error(t, err, "both source and wallet user model restrictions apply")
		})
	}
	t.Run("wallet endpoint policy", func(t *testing.T) {
		svc, stored, subs, _, _ := overflowMultiGroupFixture(t)
		_, err := svc.SelectRequestGroup(context.Background(), stored, []string{"gpt-5"}, "token", subs, func(k *APIKey) bool { return k.Group.ID != 20 }, true)
		require.Error(t, err, "a permitted source cannot bypass target endpoint restrictions")
	})
}

func TestSubscriptionOverflowMultiGroupNeverRevivesInvalidSubscription(t *testing.T) {
	for _, problem := range []error{ErrSubscriptionNotFound, ErrSubscriptionExpired, errors.New("subscription storage unavailable")} {
		t.Run(problem.Error(), func(t *testing.T) {
			svc, stored, subs, repo, _ := overflowMultiGroupFixture(t)
			repo.sub, repo.err = nil, problem
			_, err := svc.SelectRequestGroup(context.Background(), stored, []string{"gpt-5"}, "token", subs, nil, true)
			require.Error(t, err)
		})
	}
}

func TestSubscriptionOverflowMultiGroupRefreshRejectsUnboundSource(t *testing.T) {
	svc, stored, subs, _, groups := overflowMultiGroupFixture(t)
	selected, err := svc.PinAPIKeyGroup(context.Background(), stored, 10)
	require.NoError(t, err)
	effective, _, err := svc.ResolveSubscriptionBilling(context.Background(), selected, subs)
	require.NoError(t, err)
	stored.GroupIDs = []int64{5}
	_, _, err = svc.ResolveSubscriptionBilling(context.Background(), effective, subs)
	require.Error(t, err, "removing a non-primary source must revoke its wallet route too")
	require.Same(t, groups.groups[5], stored.Group)
}

func TestSubscriptionOverflowMultiGroupCacheRetainsCombinedContracts(t *testing.T) {
	svc, stored, _, _, groups := overflowMultiGroupFixture(t)
	stored.Group = groups.groups[10]
	stored.GroupID = &stored.Group.ID
	stored.Group.CodexCLIOnly, stored.Group.BlockSubagents = true, true
	snapshot := svc.snapshotFromAPIKey(context.Background(), stored)
	restored := svc.snapshotToAPIKey(stored.Key, snapshot)
	require.Equal(t, stored.GroupIDs, restored.GroupIDs)
	require.Equal(t, stored.Group.SubscriptionFallbackGroupID, restored.Group.SubscriptionFallbackGroupID)
	require.True(t, restored.Group.CodexCLIOnly)
	require.True(t, restored.Group.BlockSubagents)
	snapshot.Version = 30
	_, used, err := svc.applyAuthCacheEntry(stored.Key, &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.False(t, used, "upstream-only multi-group snapshots must reload Fork subscription policy")
}
