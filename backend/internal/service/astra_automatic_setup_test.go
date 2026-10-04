package service

import (
	"context"
	"encoding/json"
	"errors"
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
	expires := now.Add(time.Minute)
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
	expiry := now.Add(2 * time.Minute)
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "fixture", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	provider := &astraSetupUpstream{snapshot: AstraGatewayRuntime{Revision: settings.Revision,
		Sources: []AstraRouteStatus{{AccountID: 299, State: "ready", ExpiresAt: &expiry}},
		Targets: []AstraRouteStatus{{AccountID: 300, Model: "gpt-6-astra", State: "ready", ExpiresAt: &expiry}},
	}, prepare: func(context.Context) error { t.Fatal("fresh route should not trigger source generation"); return nil }}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, "ready", svc.astraSetupStatus.State)
}

func TestAutomaticBorrowRestartStartsPersistedWarm(t *testing.T) {
	settings := config.AstraRoutingSettings{AutoQuality: true, Revision: "persisted", CookiePool: config.CodexGatewayPinConfig{Enabled: true}}
	raw, _ := json.Marshal(settings)
	repo := &astraSettingsRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{astraRoutingSettingKey: string(raw)}}}
	cfg := &config.Config{}
	setting := NewSettingService(repo, cfg)
	started := make(chan struct{}, 1)
	provider := &astraSetupUpstream{prepare: func(context.Context) error { started <- struct{}{}; return errors.New("astra_source_required") }}
	svc := &AccountTestService{cfg: cfg, settingService: setting, httpUpstream: provider}
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
