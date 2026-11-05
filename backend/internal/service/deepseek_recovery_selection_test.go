//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const deepSeekRecoveryTestModel = "deepseek-v4.1-flash-1017"

func deepSeekRecoveryTestAccount(id, groupID int64) Account {
	return Account{
		ID: id, Platform: domain.PlatformDeepseek, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 2,
		GroupIDs: []int64{groupID},
		Credentials: map[string]any{
			"model_mapping":                         map[string]any{deepSeekRecoveryTestModel: deepSeekRecoveryTestModel},
			openAIEndpointCapabilitiesCredentialKey: []string{"chat_completions"},
		},
	}
}

func TestSelectDeepSeekRecoveryAccount_HardGates(t *testing.T) {
	groupID := int64(17)
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		change func(*Account)
	}{
		{"disabled", func(a *Account) { a.Status = StatusDisabled }},
		{"unschedulable", func(a *Account) { a.Schedulable = false }},
		{"wrong platform", func(a *Account) { a.Platform = PlatformOpenAI }},
		{"outside group", func(a *Account) { a.GroupIDs = []int64{18} }},
		{"ungrouped", func(a *Account) { a.GroupIDs = nil }},
		{"unsupported model", func(a *Account) { a.Credentials["model_mapping"] = map[string]any{"other": "other"} }},
		{"CC unavailable", func(a *Account) { a.Credentials[openAIEndpointCapabilitiesCredentialKey] = []string{"embeddings"} }},
		{"group model permission", func(a *Account) {
			a.AccountGroups = []AccountGroup{{GroupID: groupID, AllowedModels: []string{"deepseek-pro"}}, {GroupID: 18}}
		}},
		{"quota exhausted", func(a *Account) { a.Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0} }},
		{"daily quota exhausted", func(a *Account) {
			a.Extra = map[string]any{"quota_daily_limit": 1.0, "quota_daily_used": 1.0, "quota_daily_start": past.Format(time.RFC3339)}
		}},
		{"expired", func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past }},
		{"account rate limit", func(a *Account) { a.RateLimitResetAt = &future }},
		{"overload", func(a *Account) { a.OverloadUntil = &future }},
		{"temporary pause", func(a *Account) { a.TempUnschedulableUntil = &future }},
		{"model rate limit", func(a *Account) {
			a.Extra = map[string]any{modelRateLimitsKey: map[string]any{
				deepSeekRecoveryTestModel: map[string]any{"rate_limit_reset_at": future.Format(time.RFC3339)},
			}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := deepSeekRecoveryTestAccount(1, groupID)
			tc.change(&account)
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
				// Simple mode normally queries all groups. Recovery must still
				// enforce the caller's group and per-group AllowedModels.
				cfg: &config.Config{RunMode: config.RunModeSimple},
			}
			selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, nil)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection)
		})
	}
}

func TestSelectDeepSeekRecoveryAccount_MultiGroupAndExclusions(t *testing.T) {
	groupID := int64(17)
	a := deepSeekRecoveryTestAccount(1, groupID)
	a.GroupIDs = []int64{18, groupID}
	a.AccountGroups = []AccountGroup{{GroupID: 18, AllowedModels: []string{"other"}}, {GroupID: groupID, AllowedModels: []string{"deepseek-*"}}}
	b := deepSeekRecoveryTestAccount(2, groupID)
	b.GroupIDs = nil
	b.AccountGroups = []AccountGroup{{GroupID: 18}, {GroupID: groupID}}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{a, b}}}
	exclude := map[int64]struct{}{999: {}}
	for i := 0; i < 2; i++ {
		selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, exclude)
		require.NoError(t, err)
		require.True(t, selection.Acquired)
		require.NotNil(t, selection.ReleaseFunc)
		require.Len(t, exclude, i+1, "selection must not mutate the caller's exclusion map")
		require.NotContains(t, exclude, selection.Account.ID)
		exclude[selection.Account.ID] = struct{}{}
		selection.ReleaseFunc()
	}
	selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, exclude)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.Len(t, exclude, 3)

	// A nil group means the ungrouped pool, never arbitrary grouped accounts.
	ungrouped := deepSeekRecoveryTestAccount(3, groupID)
	ungrouped.GroupIDs = nil
	svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{a, ungrouped}}
	selection, err = svc.SelectDeepSeekRecoveryAccount(context.Background(), nil, deepSeekRecoveryTestModel, nil)
	require.NoError(t, err)
	require.Equal(t, ungrouped.ID, selection.Account.ID)
	selection.ReleaseFunc()
}

func TestSelectDeepSeekRecoveryAccount_PrefersFreeSlotOverAffinityAndPriority(t *testing.T) {
	groupID := int64(17)
	accounts := []Account{deepSeekRecoveryTestAccount(1, groupID), deepSeekRecoveryTestAccount(2, groupID)}
	accounts[0].Priority, accounts[1].Priority = -100, 100
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"session": 1, "parent": 1}}
	acquired, released := []int64{}, []int64{}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:       cache, cfg: newSchedulerTestSubscriptionPriorityConfig(),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			// Even a stale "full" load reading must not hide an available slot.
			loadMap:     map[int64]*AccountLoadInfo{1: {CurrentConcurrency: 2, LoadRate: 100}, 2: {CurrentConcurrency: 2, LoadRate: 100}},
			acquiredIDs: &acquired, releasedIDs: &released,
		}),
	}
	ctx := context.WithValue(context.Background(), ctxkey.PrefetchedStickyAccountID, int64(1))
	ctx = context.WithValue(ctx, ctxkey.PrefetchedStickyGroupID, groupID)
	ctx = context.WithValue(ctx, openAIGuardianParentAffinityContextKey{}, openAIGuardianParentAffinity{currentSessionHash: "parent"})
	selection, err := svc.SelectDeepSeekRecoveryAccount(ctx, &groupID, deepSeekRecoveryTestModel, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), selection.Account.ID)
	require.True(t, selection.Acquired)
	require.NotEmpty(t, selection.AccountRequestID)
	require.Nil(t, selection.WaitPlan)
	require.False(t, selection.stickySessionHit)
	selection.ReleaseFunc()
	require.Equal(t, []int64{2}, released)
	require.Equal(t, map[string]int64{"session": 1, "parent": 1}, cache.sessionBindings)
	require.Empty(t, cache.deletedSessions)
}

func TestSelectDeepSeekRecoveryAccount_EntirePoolBeforeBoundedWait(t *testing.T) {
	groupID := int64(17)
	var accounts []Account
	busy := map[int64]bool{}
	for id := int64(1); id <= 80; id++ {
		a := deepSeekRecoveryTestAccount(id, groupID)
		a.Priority = int(id)
		accounts = append(accounts, a)
		busy[id] = false
	}
	accounts = append(accounts, accounts[1]) // a joined row must not get a second probe
	acquired := []int64{}
	cfg := newSchedulerTestSubscriptionPriorityConfig() // TopK = 1
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 7 * time.Second
	cfg.Gateway.Scheduling.FallbackMaxWaiting = 3
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cfg: cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: busy, acquiredIDs: &acquired}),
	}
	exclude := map[int64]struct{}{1: {}}
	selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, exclude)
	require.NoError(t, err)
	require.False(t, selection.Acquired)
	require.Nil(t, selection.ReleaseFunc)
	require.Empty(t, selection.AccountRequestID)
	require.Len(t, acquired, 79, "recovery must not truncate to TopK or the normal probe budget")
	seen := map[int64]bool{}
	for _, id := range acquired {
		require.NotEqual(t, int64(1), id)
		require.False(t, seen[id], "duplicate slot probe for %d", id)
		seen[id] = true
	}
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, acquired[0], selection.Account.ID, "the random order also determines fallback wait")
	require.Equal(t, &AccountWaitPlan{AccountID: selection.Account.ID, MaxConcurrency: 2, Timeout: 7 * time.Second, MaxWaiting: 3}, selection.WaitPlan)
	require.Equal(t, map[int64]struct{}{1: {}}, exclude)
}

func TestSelectDeepSeekRecoveryAccount_FreshDBRecheckReleasesInvalidSlot(t *testing.T) {
	groupID := int64(17)
	for _, tc := range []struct {
		name   string
		change func(*Account)
	}{
		{"removed from group", func(a *Account) { a.GroupIDs = []int64{18} }},
		{"manually paused", func(a *Account) { a.Schedulable = false }},
		{"quota consumed", func(a *Account) { a.Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0} }},
		{"model permission revoked", func(a *Account) {
			a.AccountGroups = []AccountGroup{{GroupID: groupID, AllowedModels: []string{"other"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stale := deepSeekRecoveryTestAccount(1, groupID)
			latest := deepSeekRecoveryTestAccount(1, groupID)
			tc.change(&latest)
			acquired, released := []int64{}, []int64{}
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{latest}},
				cfg:         &config.Config{RunMode: config.RunModeSimple},
				schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
					snapshotAccounts: []*Account{&stale}, accountsByID: map[int64]*Account{1: &stale},
				}},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released}),
			}
			selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, nil)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection)
			require.Equal(t, []int64{1}, acquired)
			require.Equal(t, acquired, released, "rejected acquired slot must be released")
		})
	}
}

func TestSelectDeepSeekRecoveryAccount_PreservesProfitGate(t *testing.T) {
	groupID := int64(17)
	cheap, expensive := deepSeekRecoveryTestAccount(1, groupID), deepSeekRecoveryTestAccount(2, groupID)
	profitControlTestAccountWithRate(&cheap, 0.2)
	profitControlTestAccountWithRate(&expensive, 0.9)
	gate := &openAIProfitControlGate{groupID: groupID, platform: domain.PlatformDeepseek, threshold: 0.5, pricingAt: time.Now()}
	ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, gate)
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{cheap, expensive}}}
	selection, err := svc.SelectDeepSeekRecoveryAccount(ctx, &groupID, deepSeekRecoveryTestModel, nil)
	require.NoError(t, err)
	require.Equal(t, cheap.ID, selection.Account.ID)
	require.Same(t, gate, selection.profitGate)
	require.Same(t, gate, ContextWithSelectionProfitGate(context.Background(), selection).Value(openAIProfitControlGateCtxKey{}))
	selection.ReleaseFunc()
	selection, err = svc.SelectDeepSeekRecoveryAccount(ctx, &groupID, deepSeekRecoveryTestModel, map[int64]struct{}{cheap.ID: {}})
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
}

func TestSelectDeepSeekRecoveryAccount_RequiredBPSDoesNotFallBackToNative(t *testing.T) {
	groupID := int64(17)
	protected := deepSeekRecoveryTestAccount(1, groupID)
	protected.Extra = map[string]any{
		ExcelBPSRequiredGroupIDsKey: []int64{groupID},
		ExcelBPSRequiredModelsKey:   []string{deepSeekRecoveryTestModel},
	}
	native := deepSeekRecoveryTestAccount(2, groupID)
	acquired := []int64{}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{protected, native}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired}),
	}
	selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, map[int64]struct{}{protected.ID: {}})
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
	require.Empty(t, acquired, "excluding a protected account must not bypass a BPS-only group/model")
}

func TestSelectDeepSeekRecoveryAccount_ChannelModelRestriction(t *testing.T) {
	groupID := int64(17)
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceUpstream} {
		t.Run(source, func(t *testing.T) {
			blocked := deepSeekRecoveryTestAccount(1, groupID)
			allowed := deepSeekRecoveryTestAccount(2, groupID)
			allowed.Credentials["model_mapping"] = map[string]any{deepSeekRecoveryTestModel: "allowed-upstream"}
			channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, RestrictModels: true, BillingModelSource: source,
				ModelPricing: []ChannelModelPricing{{Platform: domain.PlatformDeepseek, Models: []string{"allowed-upstream"}}},
			}
			acquired := []int64{}
			svc := &OpenAIGatewayService{
				accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{blocked, allowed}},
				channelService:     newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: domain.PlatformDeepseek})),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired}),
			}
			selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, nil)
			if source == BillingModelSourceRequested {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, selection)
				require.Empty(t, acquired)
				return
			}
			require.NoError(t, err)
			require.Equal(t, allowed.ID, selection.Account.ID)
			require.Equal(t, []int64{allowed.ID}, acquired)
			selection.ReleaseFunc()
		})
	}
}

type deepSeekRecoveryAcquireCache struct {
	schedulerTestConcurrencyCache
	onAcquire func(context.Context, int64, int, string) (bool, error)
}

func (c deepSeekRecoveryAcquireCache) AcquireAccountSlot(ctx context.Context, id int64, limit int, requestID string) (bool, error) {
	return c.onAcquire(ctx, id, limit, requestID)
}

func TestSelectDeepSeekRecoveryAccount_ReacquiresChangedConcurrencyLimit(t *testing.T) {
	groupID := int64(17)
	stale := deepSeekRecoveryTestAccount(1, groupID)
	latest := deepSeekRecoveryTestAccount(1, groupID)
	latest.Concurrency = 1
	limits, released := []int{}, []int64{}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{latest}},
		schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
			snapshotAccounts: []*Account{&stale}, accountsByID: map[int64]*Account{1: &stale},
		}},
		concurrencyService: NewConcurrencyService(deepSeekRecoveryAcquireCache{
			schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{releasedIDs: &released},
			onAcquire: func(_ context.Context, _ int64, limit int, _ string) (bool, error) {
				limits = append(limits, limit)
				return true, nil
			},
		}),
	}
	selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, nil)
	require.NoError(t, err)
	require.Equal(t, []int{2, 1}, limits)
	require.Equal(t, 1, selection.Account.Concurrency)
	require.Equal(t, []int64{1}, released, "the stale-limit slot must be released before reacquisition")
	selection.ReleaseFunc()
	require.Equal(t, []int64{1, 1}, released)
}

func TestSelectDeepSeekRecoveryAccount_CancellationAndSlotError(t *testing.T) {
	groupID := int64(17)
	account := deepSeekRecoveryTestAccount(1, groupID)
	for _, tc := range []struct {
		name    string
		pre     bool
		slotErr bool
	}{
		{name: "cancelled before selection", pre: true},
		{name: "cancelled after acquiring slot"},
		{name: "slot authority unavailable", slotErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := context.Canceled
			if tc.slotErr {
				wantErr = errors.New("slot cache unavailable")
			}
			if tc.pre {
				cancel()
			}
			calls, released := 0, []int64{}
			cache := deepSeekRecoveryAcquireCache{
				schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{releasedIDs: &released},
				onAcquire: func(context.Context, int64, int, string) (bool, error) {
					calls++
					if tc.slotErr {
						return false, wantErr
					}
					cancel()
					return true, nil
				},
			}
			svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}}, concurrencyService: NewConcurrencyService(cache)}
			selection, err := svc.SelectDeepSeekRecoveryAccount(ctx, &groupID, deepSeekRecoveryTestModel, nil)
			require.ErrorIs(t, err, wantErr)
			require.Nil(t, selection)
			if tc.pre {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
			if !tc.pre && !tc.slotErr {
				require.Equal(t, []int64{1}, released)
			} else {
				require.Empty(t, released)
			}
		})
	}
}

// The repository cache imports service, so this tiny adapter uses miniredis for
// the GatewayCache write boundary without introducing an import cycle.
type deepSeekRecoveryRedisStickyCache struct {
	schedulerTestGatewayCache
	client *redis.Client
	reads  int
	writes int
}

func (c *deepSeekRecoveryRedisStickyCache) SetSessionAccountID(ctx context.Context, groupID int64, hash string, id int64, ttl time.Duration) error {
	c.writes++
	return c.client.Set(ctx, fmt.Sprintf("sticky:%d:%s", groupID, hash), id, ttl).Err()
}

func (c *deepSeekRecoveryRedisStickyCache) GetSessionAccountID(ctx context.Context, groupID int64, hash string) (int64, error) {
	c.reads++
	return c.client.Get(ctx, fmt.Sprintf("sticky:%d:%s", groupID, hash)).Int64()
}

func TestWithDeepSeekRecoverySelection_PreservesStickyThroughAdmission(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &deepSeekRecoveryRedisStickyCache{client: client}
	groupID := int64(17)
	svc := &OpenAIGatewayService{cache: cache, cfg: &config.Config{}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{ID: groupID, Platform: domain.PlatformDeepseek})
	pricingAt := time.Now()
	ctx = context.WithValue(ctx, openAIPricingAtCtxKey{}, pricingAt)
	require.NoError(t, svc.bindOpenAIStickySessionDuringSelection(ctx, &groupID, "original-session", 1))
	require.Equal(t, 1, cache.writes)
	keys := server.Keys()
	require.Len(t, keys, 1)
	beforeTTL := server.TTL(keys[0])

	retryCtx := WithDeepSeekRecoverySelection(ctx)
	require.Equal(t, pricingAt, OpenAIPricingAtFromContext(retryCtx))
	require.Same(t, ctx.Value(ctxkey.Group), retryCtx.Value(ctxkey.Group))
	svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{deepSeekRecoveryTestAccount(2, groupID)}}
	selection, err := svc.SelectDeepSeekRecoveryAccount(retryCtx, &groupID, deepSeekRecoveryTestModel, map[int64]struct{}{1: {}})
	require.NoError(t, err)
	require.Equal(t, int64(2), selection.Account.ID)
	selection.ReleaseFunc()
	for _, attemptCtx := range []context.Context{
		retryCtx,
		context.WithValue(retryCtx, openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{groupID: groupID}),
	} {
		require.NoError(t, svc.bindOpenAIStickySessionDuringSelection(attemptCtx, &groupID, "original-session", 2))
		require.NoError(t, svc.BindStickySessionAfterProfitAdmission(attemptCtx, &groupID, "original-session", 2))
		require.NoError(t, svc.BindStickySessionAfterProfitAdmission(attemptCtx, &groupID, "unbound-session", 2))
	}
	require.Equal(t, 1, cache.writes)
	require.Zero(t, cache.reads)
	require.Equal(t, keys, server.Keys())
	require.Equal(t, beforeTTL, server.TTL(keys[0]))
	value, err := server.Get(keys[0])
	require.NoError(t, err)
	require.Equal(t, "1", value)

	// The marker is local to recovery: ordinary admission still writes normally.
	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, &groupID, "original-session", 3))
	require.Equal(t, 2, cache.writes)
	value, err = server.Get(keys[0])
	require.NoError(t, err)
	require.Equal(t, "3", value)
}

func TestRevalidateDeepSeekRecoveryAccount_AfterWaiting(t *testing.T) {
	groupID := int64(17)
	for _, cached := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			change func(*Account)
			allow  bool
		}{
			{"stopped scheduling", func(a *Account) { a.Schedulable = false }, false},
			{"disabled", func(a *Account) { a.Status = StatusDisabled }, false},
			{"moved to another group", func(a *Account) { a.GroupIDs = []int64{18} }, false},
			{"model permission narrowed", func(a *Account) {
				a.AccountGroups = []AccountGroup{{GroupID: groupID, AllowedModels: []string{"other"}}, {GroupID: 18}}
			}, false},
			{"quota exhausted", func(a *Account) { a.Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0} }, false},
			{"model removed", func(a *Account) { a.Credentials["model_mapping"] = map[string]any{"other": "other"} }, false},
			{"CC permission removed", func(a *Account) { a.Credentials[openAIEndpointCapabilitiesCredentialKey] = []string{"embeddings"} }, false},
			{"concurrency reduced", func(a *Account) { a.Concurrency = 1 }, false},
			{"platform changed", func(a *Account) { a.Platform = PlatformOpenAI }, false},
			{"protected BPS policy enabled", func(a *Account) {
				a.Extra = map[string]any{ExcelBPSRequiredGroupIDsKey: []int64{groupID}, ExcelBPSRequiredModelsKey: []string{deepSeekRecoveryTestModel}}
			}, false},
			{"still eligible with refreshed credentials", func(a *Account) {
				a.Credentials["api_key"] = "sk-refreshed-fixture"
				a.GroupIDs = []int64{18, groupID}
				a.AccountGroups = []AccountGroup{{GroupID: 18, AllowedModels: []string{"other"}}, {GroupID: groupID, AllowedModels: []string{"deepseek-*"}}}
			}, true},
		} {
			t.Run(fmt.Sprintf("cached=%t/%s", cached, tc.name), func(t *testing.T) {
				accounts := []Account{deepSeekRecoveryTestAccount(1, groupID)}
				acquired, released := []int64{}, []int64{}
				sticky := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:session": 1}}
				svc := &OpenAIGatewayService{
					accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
					cache:       sticky, cfg: &config.Config{RunMode: config.RunModeSimple},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
						acquireResults: map[int64]bool{1: false}, acquiredIDs: &acquired, releasedIDs: &released,
					}),
				}
				if cached {
					stale := deepSeekRecoveryTestAccount(1, groupID)
					svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
						snapshotAccounts: []*Account{&stale}, accountsByID: map[int64]*Account{1: &stale},
					}}
				}
				selection, err := svc.SelectDeepSeekRecoveryAccount(context.Background(), &groupID, deepSeekRecoveryTestModel, nil)
				require.NoError(t, err)
				require.NotNil(t, selection.WaitPlan)
				require.False(t, selection.Acquired)
				waiting := *selection.Account
				// A primary update during the wait leaves the selected account and
				// ordinary scheduler snapshot stale, including in simple mode.
				accounts[0] = deepSeekRecoveryTestAccount(1, groupID)
				tc.change(&accounts[0])
				latest, err := svc.RevalidateDeepSeekRecoveryAccount(context.Background(), &groupID, &waiting, deepSeekRecoveryTestModel)
				if tc.allow {
					require.NoError(t, err)
					require.Equal(t, "sk-refreshed-fixture", latest.Credentials["api_key"])
					require.Equal(t, accounts[0].AccountGroups, latest.AccountGroups)
				} else {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Nil(t, latest)
				}
				require.Equal(t, []int64{1}, acquired, "revalidation must not acquire another slot")
				require.Empty(t, released, "the caller owns the slot release")
				require.Equal(t, map[string]int64{"openai:session": 1}, sticky.sessionBindings)
				require.Empty(t, sticky.deletedSessions)
			})
		}
	}
}

type deepSeekRecoveryRefreshRepo struct {
	schedulerTestOpenAIAccountRepo
	read func(context.Context, int64) (*Account, error)
}

func (r deepSeekRecoveryRefreshRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.read(ctx, id)
}

func TestRevalidateDeepSeekRecoveryAccount_FailsClosedOnUnavailableAuthority(t *testing.T) {
	groupID := int64(17)
	selected := deepSeekRecoveryTestAccount(1, groupID)
	readErr := errors.New("account authority unavailable")
	for _, tc := range []struct {
		name     string
		read     func(context.Context, int64) (*Account, error)
		regional bool
	}{
		{"DB error", func(context.Context, int64) (*Account, error) { return nil, readErr }, false},
		{"deleted account", func(context.Context, int64) (*Account, error) { return nil, nil }, false},
		{"wrong account identity", func(context.Context, int64) (*Account, error) {
			a := deepSeekRecoveryTestAccount(2, groupID)
			return &a, nil
		}, false},
		{"regional details unavailable", func(context.Context, int64) (*Account, error) {
			t.Fatal("regional failure must not fall back to the central repository")
			return nil, nil
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{
				accountRepo:       deepSeekRecoveryRefreshRepo{read: tc.read},
				schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{1: &selected}}},
			}
			if tc.regional {
				svc.schedulerSnapshot.localSnapshotReader = NewLocalSchedulerSnapshotStore(nil)
			}
			latest, err := svc.RevalidateDeepSeekRecoveryAccount(context.Background(), &groupID, &selected, deepSeekRecoveryTestModel)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, latest, "an unavailable authority must not revive the selected/cached account")
		})
	}
	latest, err := (&OpenAIGatewayService{}).RevalidateDeepSeekRecoveryAccount(context.Background(), &groupID, &selected, deepSeekRecoveryTestModel)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, latest)
}

func TestRevalidateDeepSeekRecoveryAccount_PreservesCancellationAndProfitGate(t *testing.T) {
	groupID := int64(17)
	selected := deepSeekRecoveryTestAccount(1, groupID)
	accounts := []Account{deepSeekRecoveryTestAccount(1, groupID)}
	profitControlTestAccountWithRate(&accounts[0], 0.9)
	gate := &openAIProfitControlGate{groupID: groupID, platform: domain.PlatformDeepseek, threshold: 0.5, pricingAt: time.Now()}
	ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, gate)
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}}
	latest, err := svc.RevalidateDeepSeekRecoveryAccount(ctx, &groupID, &selected, deepSeekRecoveryTestModel)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, latest)
	require.Same(t, gate, ctx.Value(openAIProfitControlGateCtxKey{}))

	ctx, cancel := context.WithCancel(context.Background())
	svc.accountRepo = deepSeekRecoveryRefreshRepo{read: func(readCtx context.Context, id int64) (*Account, error) {
		deadline, ok := readCtx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 3*time.Second)
		cancel()
		return &selected, nil
	}}
	defer cancel()
	latest, err = svc.RevalidateDeepSeekRecoveryAccount(ctx, &groupID, &selected, deepSeekRecoveryTestModel)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, latest)
}
