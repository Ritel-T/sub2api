package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type astraSetupUpstream struct {
	HTTPUpstream
	prepare  func(context.Context) error
	snapshot AstraGatewayRuntime
}

func TestAutomaticBorrowWarmOnlyExpiredModelsAndHonorsRetry(t *testing.T) {
	now := time.Now()
	expires := now.Add(2*time.Minute + time.Second)
	retry := now.Add(5 * time.Minute)
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{
		{AccountID: 1, Model: "gpt-6-astra", State: "ready", ExpiresAt: &expires},
		{AccountID: 1, Model: "gpt-6.1-sol", State: "rejected", RetryAt: &retry},
	}}
	require.False(t, borrowModelNeedsWarm(snapshot, 1, "gpt-6-astra", now))
	require.False(t, borrowModelNeedsWarm(snapshot, 1, "gpt-6.1-sol", now))
	require.True(t, borrowModelNeedsWarm(snapshot, 1, "gpt-6-astra", now.Add(45*time.Second)))
	require.True(t, borrowModelNeedsWarm(snapshot, 1, "gpt-6.1-sol", now.Add(6*time.Minute)))
	require.True(t, borrowModelNeedsWarm(snapshot, 2, "gpt-6-astra", now))
}

func TestAutomaticBorrowReadyCycleDoesNotProbeAgain(t *testing.T) {
	now := time.Now()
	expiry := now.Add(2*time.Minute + time.Second)
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "fixture", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	provider := &astraSetupUpstream{snapshot: AstraGatewayRuntime{Revision: settings.Revision,
		Sources: []AstraRouteStatus{{AccountID: 299, State: "ready", ExpiresAt: &expiry}},
		Targets: []AstraRouteStatus{{AccountID: 300, Model: "gpt-6-astra", State: "ready", ExpiresAt: &expiry}},
	}, prepare: func(context.Context) error { t.Fatal("fresh route should not trigger source generation"); return nil }}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &astraWarmAccountRepo{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, "ready", svc.astraSetupStatus.State)
}

func TestAutomaticBorrowRestartStartsPersistedWarm(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "persisted", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	raw, _ := json.Marshal(settings)
	repo := &astraSettingsRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{astraRoutingSettingKey: string(raw)}}}
	cfg := &config.Config{}
	setting := NewSettingService(repo, cfg)
	started := make(chan struct{}, 1)
	provider := &astraSetupUpstream{prepare: func(context.Context) error { started <- struct{}{}; return errors.New("astra_source_required") }}
	svc := &AccountTestService{cfg: cfg, settingService: setting, httpUpstream: provider, accountRepo: &astraWarmAccountRepo{}}
	require.NoError(t, svc.StartPersistedAutomaticGatewayBorrow())
	defer svc.StopAstraAutomaticSetup()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("persisted automatic settings never started route warm")
	}
}

func (s *astraSetupUpstream) PrepareAstraGateway(ctx context.Context) error              { return s.prepare(ctx) }
func (s *astraSetupUpstream) SetAstraGatewayPreparer(func(context.Context, int64) error) {}
func (s *astraSetupUpstream) AstraGatewaySnapshot(context.Context) AstraGatewayRuntime {
	return s.snapshot
}
func TestAstraAutomaticSetupFailureAndDisable(t *testing.T) {
	cfg := &config.Config{}
	values := config.AstraRoutingSettings{Revision: "one", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return values })
	svc := &AccountTestService{cfg: cfg, httpUpstream: &astraSetupUpstream{prepare: func(context.Context) error { return errors.New("no_qualified_source_route") }}}
	svc.StartAstraAutomaticSetup(values)
	require.Eventually(t, func() bool {
		svc.astraSetupMu.Lock()
		defer svc.astraSetupMu.Unlock()
		return svc.astraSetupStatus.State == "failed"
	}, time.Second, time.Millisecond)
	svc.astraSetupMu.Lock()
	require.Equal(t, "source", svc.astraSetupStatus.Phase)
	require.Equal(t, "no_qualified_source_route", svc.astraSetupStatus.Reason)
	svc.astraSetupMu.Unlock()
	// Complete worker before mutating the test loader's settings.
	svc.astraGatewayActionMu.Lock()
	values.Revision = "two"
	values.CookiePool.Enabled = false
	svc.astraGatewayActionMu.Unlock()
	svc.StartAstraAutomaticSetup(values)
	svc.astraSetupMu.Lock()
	require.Equal(t, "disabled", svc.astraSetupStatus.State)
	svc.astraSetupMu.Unlock()
}

func TestAstraSetupRequiresAllTargetsOnCurrentRoutes(t *testing.T) {
	provider := &astraSetupUpstream{snapshot: AstraGatewayRuntime{Targets: []AstraRouteStatus{{AccountID: 300, State: "waiting"}, {AccountID: 301, State: "ready"}}}}
	id, ready := astraTargetsReady(t.Context(), provider, []int64{300, 301})
	require.False(t, ready)
	require.Equal(t, int64(300), id)
	provider.snapshot.Targets[0].State = "ready"
	_, ready = astraTargetsReady(t.Context(), provider, []int64{300, 301})
	require.True(t, ready)
}

func TestAstraSetupWaitsForConcurrentPreparation(t *testing.T) {
	calls := 0
	provider := &astraSetupUpstream{prepare: func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("preparation_in_progress")
		}
		return nil
	}}
	require.NoError(t, prepareAstraForSetup(t.Context(), provider))
	require.Equal(t, 2, calls)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, prepareAstraForSetup(ctx, provider), context.Canceled)
	require.Equal(t, 2, calls)
}

func TestAutomaticBorrowWarmPrioritizesExactModelExpiry(t *testing.T) {
	now := time.Now()
	early := now.Add(25 * time.Second)
	later := now.Add(100 * time.Second)
	expired := now.Add(-time.Second)
	checked := now.Add(-time.Hour)
	pool := config.CodexGatewayPinConfig{TargetAccountIDs: []int64{34, 88, 109, 140}, Models: []string{"gpt-6.1-sol", "gpt-6-astra"}}
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{
		{AccountID: 34, Model: "gpt-6.1-sol", State: "rejected", CheckedAt: &checked},
		{AccountID: 88, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &early},
		{AccountID: 109, Model: "gpt-6.1-sol", State: "ready", Reason: "target_probe_passed", ExpiresAt: &later},
		{AccountID: 140, Model: "gpt-6-astra", State: "expired", Reason: "target_probe_passed", ExpiresAt: &expired},
	}}
	plan := automaticBorrowWarmPlan(snapshot, pool, now)
	require.Len(t, plan, 2)
	require.Equal(t, int64(88), plan[0].id)
	require.Equal(t, "gpt-6-astra", plan[0].models[0].model)
	require.Equal(t, int64(109), plan[1].id)
	require.Equal(t, "gpt-6.1-sol", plan[1].models[0].model)
	pool.TargetAccountIDs = []int64{34, 140}
	plan = automaticBorrowWarmPlan(snapshot, pool, now)
	require.Equal(t, int64(140), plan[0].id, "expired passed witness precedes first/failed probes")
	require.Equal(t, "gpt-6-astra", plan[0].models[0].model, "account's expired passed model precedes unverified Sol")
}

func TestAutomaticBorrowTargetWarmFreshRetryAndOldInitialFairness(t *testing.T) {
	now := time.Now()
	fresh := now.Add(121 * time.Second)
	boundary := now.Add(120 * time.Second)
	retry := now.Add(time.Minute)
	old := now.Add(-time.Hour)
	recent := now.Add(-time.Second)
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{
		{AccountID: 34, Model: "gpt-6-astra", State: "rejected", CheckedAt: &recent},
		{AccountID: 88, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &fresh},
		{AccountID: 109, Model: "gpt-6-astra", State: "rejected", RetryAt: &retry},
		{AccountID: 140, Model: "gpt-6-astra", State: "rejected", CheckedAt: &old},
	}}
	pool := config.CodexGatewayPinConfig{TargetAccountIDs: []int64{34, 88, 109, 140, 111}, Models: []string{"gpt-6-astra"}}
	require.False(t, borrowModelNeedsWarm(snapshot, 88, "gpt-6-astra", now))
	snapshot.Targets[1].ExpiresAt = &boundary
	require.True(t, borrowModelNeedsWarm(snapshot, 88, "gpt-6-astra", now))
	snapshot.Targets[1].ExpiresAt = &fresh
	plan := automaticBorrowWarmPlan(snapshot, pool, now)
	require.Len(t, plan, 2)
	require.Equal(t, int64(111), plan[0].id, "untried account precedes recently failed one")
	require.Equal(t, int64(140), plan[1].id, "oldest attempted initial model next")
}

func TestAutomaticBorrowSlowInitialFailureCannotDelayReadyRenewals(t *testing.T) {
	now := time.Now()
	expiry := now.Add(30 * time.Second)
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{
		{AccountID: 88, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry},
		{AccountID: 109, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry},
	}}
	pool := config.CodexGatewayPinConfig{TargetAccountIDs: []int64{34, 88, 109}, Models: []string{"gpt-6.1-sol", "gpt-6-astra"}}
	var mu sync.Mutex
	calls := map[int64][]string{}
	_, err := runAutomaticBorrowWarmTargets(t.Context(), snapshot, pool, now, func(_ context.Context, id int64, model string) error {
		mu.Lock()
		calls[id] = append(calls[id], model)
		mu.Unlock()
		if id == 34 {
			t.Error("slow first target must not occupy the two renewal slots")
		}
		return nil
	})
	require.NoError(t, err)
	require.NotContains(t, calls, int64(34))
	require.Equal(t, []string{"gpt-6-astra", "gpt-6.1-sol"}, calls[88])
	require.Equal(t, []string{"gpt-6-astra", "gpt-6.1-sol"}, calls[109])
}

func TestAutomaticBorrowTargetBudgetReturnsAfterPartialRenewal(t *testing.T) {
	now := time.Now()
	expiry := now.Add(30 * time.Second)
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{{AccountID: 88, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry}}}
	pool := config.CodexGatewayPinConfig{TargetAccountIDs: []int64{88, 34, 140}, Models: []string{"gpt-6.1-sol", "gpt-6-astra"}}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	calls := map[int64][]string{}
	started := time.Now()
	_, err := runAutomaticBorrowWarmTargets(ctx, snapshot, pool, now, func(callCtx context.Context, id int64, model string) error {
		mu.Lock()
		calls[id] = append(calls[id], model)
		mu.Unlock()
		if id == 88 && model == "gpt-6-astra" {
			return nil
		}
		<-callCtx.Done()
		return callCtx.Err()
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), time.Second)
	require.Equal(t, "gpt-6-astra", calls[88][0])
	require.NotContains(t, calls, int64(140), "no third account or extra retry in this window")
	require.Equal(t, 120*time.Second, automaticBorrowTargetWarmBudget)
}

func TestAutomaticBorrowWarmSkipsUnavailableAndQuotaAccounts(t *testing.T) {
	now := time.Now()
	retry := now.Add(time.Hour)
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{
		{AccountID: 34, Model: "gpt-6-astra", State: "blocked", Reason: "target_account_unavailable"},
		{AccountID: 111, Model: "gpt-6-astra", State: "blocked", Reason: "target_account_rate_limited", RetryAt: &retry},
	}}
	pool := config.CodexGatewayPinConfig{TargetAccountIDs: []int64{34, 111, 88, 140}, Models: []string{"gpt-6-astra"}}
	plan := automaticBorrowWarmPlan(snapshot, pool, now)
	require.Len(t, plan, 2)
	require.Equal(t, int64(88), plan[0].id)
	require.Equal(t, int64(140), plan[1].id)
}

type astraWarmAccountRepo struct{ AccountRepository }

func (r *astraWarmAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}, nil
}

func TestAutomaticBorrowAllQuotaBlockedDoesNotPrepareSources(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "blocked", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	until := time.Now().Add(time.Hour)
	provider := &astraSetupUpstream{snapshot: AstraGatewayRuntime{Revision: settings.Revision, Targets: []AstraRouteStatus{{AccountID: 300, Model: "gpt-6-astra", State: "waiting"}}}, prepare: func(context.Context) error { t.Fatal("blocked targets must not acquire source Cookies"); return nil }}
	account := &Account{ID: 300, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, RateLimitResetAt: &until}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &astraWarmSpecificAccountRepo{account: account}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, "waiting", svc.astraSetupStatus.State)
	require.Equal(t, "exploration_cooling", svc.astraSetupStatus.Reason)
	require.Equal(t, 1, svc.astraSetupStatus.Blocked)
}

type astraWarmSpecificAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *astraWarmSpecificAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func TestAutomaticBorrowStatusPartialBlockedAndTargetBudget(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Minute)
	retry := now.Add(time.Hour)
	pool := config.CodexGatewayPinConfig{TargetAccountIDs: []int64{88, 111}, Models: []string{"gpt-6-astra"}}
	snapshot := AstraGatewayRuntime{Targets: []AstraRouteStatus{{AccountID: 88, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry}, {AccountID: 111, Model: "gpt-6-astra", State: "blocked", RetryAt: &retry}}}
	result := automaticBorrowCycleStatus(snapshot, pool, now, nil, nil)
	require.Equal(t, "partial", result.State)
	require.Equal(t, 1, result.Ready)
	require.Equal(t, 1, result.Blocked)
	require.Zero(t, result.Pending)
	result = automaticBorrowCycleStatus(snapshot, pool, now, context.DeadlineExceeded, nil)
	require.Equal(t, "waiting", result.State)
	require.Equal(t, "warm_budget_exhausted", result.Reason)
	require.Equal(t, 1, result.Ready, "partial route retains its own readiness")
	result = automaticBorrowCycleStatus(snapshot, pool, now, context.Canceled, context.Canceled)
	require.Equal(t, "setup_cancelled", result.Reason)
	require.Equal(t, "failed", result.State)
}

func TestAutomaticBorrowNewCycleResetsTimestampsBeforeSourcePreparation(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "timestamps", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	old := time.Now().Add(-time.Hour)
	var svc *AccountTestService
	provider := &astraSetupUpstream{prepare: func(context.Context) error {
		require.True(t, svc.astraSetupStatus.StartedAt.After(old))
		require.Nil(t, svc.astraSetupStatus.FinishedAt)
		require.Equal(t, "running", svc.astraSetupStatus.State)
		return errors.New("no_qualified_source_route")
	}}
	svc = &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &astraWarmAccountRepo{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision, State: "failed", StartedAt: old, FinishedAt: &old}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, "waiting", svc.astraSetupStatus.State)
	require.NotNil(t, svc.astraSetupStatus.FinishedAt)
	require.Equal(t, "no_qualified_source_route", svc.astraSetupStatus.Reason)
}

func TestAutomaticBorrowFreshAndQuotaModelsReportPartialWithoutSourceFetch(t *testing.T) {
	now := time.Now()
	expiry := now.Add(3 * time.Minute)
	retry := now.Add(time.Hour)
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "partial", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	provider := &astraSetupUpstream{snapshot: AstraGatewayRuntime{Revision: settings.Revision, Targets: []AstraRouteStatus{{AccountID: 300, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry}, {AccountID: 300, Model: "gpt-6.1-sol", State: "rejected", Reason: "target_probe_rate_limited", RetryAt: &retry}}}, prepare: func(context.Context) error {
		t.Fatal("no executable target: source preparation is unnecessary")
		return nil
	}}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &astraWarmAccountRepo{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, "partial", svc.astraSetupStatus.State)
	require.Equal(t, "target_partially_ready", svc.astraSetupStatus.Reason)
	require.Equal(t, 1, svc.astraSetupStatus.Ready)
	require.Equal(t, 1, svc.astraSetupStatus.Blocked)
}

func TestAutomaticBorrowSourceShortageOnlyAutoIsWaiting(t *testing.T) {
	for _, tc := range []struct {
		name          string
		auto          bool
		reason, state string
	}{
		{"automatic shortage", true, "no_qualified_source_route", "waiting"},
		{"automatic cooldown", true, "source_probe_cooldown", "waiting"},
		{"automatic source quota", true, "source_account_rate_limited", "waiting"},
		{"automatic source unavailable", true, "source_account_unavailable", "waiting"},
		{"missing preparer", true, "source_preparer_unavailable", "failed"},
		{"runtime", true, "runtime_unavailable", "failed"},
		{"unknown network error", true, "fixture_network_failure", "failed"},
		{"manual shortage", false, "no_qualified_source_route", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := config.AstraRoutingSettings{AutoQuality: tc.auto, Revision: "source-shortage", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
			cfg := &config.Config{}
			cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
			provider := &astraSetupUpstream{prepare: func(context.Context) error { return errors.New(tc.reason) }}
			svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &astraWarmAccountRepo{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
			ctx, cancel := context.WithCancel(t.Context())
			svc.runAstraAutomaticSetup(ctx, cancel, settings)
			require.Equal(t, tc.state, svc.astraSetupStatus.State)
			require.Equal(t, "source", svc.astraSetupStatus.Phase)
			require.Zero(t, svc.astraSetupStatus.Ready)
			if tc.state == "waiting" {
				require.Equal(t, tc.reason, svc.astraSetupStatus.Reason)
				require.Equal(t, 1, svc.astraSetupStatus.Pending)
			}
		})
	}
}
func TestAutomaticBorrowSourceShortageRetainsExistingRoutesAsPartial(t *testing.T) {
	expiry := time.Now().Add(time.Minute)
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "partial-source", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	provider := &astraSetupUpstream{snapshot: AstraGatewayRuntime{Revision: settings.Revision, Targets: []AstraRouteStatus{{AccountID: 300, Model: "gpt-6-astra", State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry}}}, prepare: func(context.Context) error { return errors.New("no_qualified_source_route") }}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &astraWarmAccountRepo{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, "partial", svc.astraSetupStatus.State)
	require.Equal(t, "no_qualified_source_route", svc.astraSetupStatus.Reason)
	require.Equal(t, 1, svc.astraSetupStatus.Ready)
	require.Equal(t, expiry, *provider.snapshot.Targets[0].ExpiresAt, "source failure cannot clear or extend the retained route")
}
