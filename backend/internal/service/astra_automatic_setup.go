package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type AstraSetupStatus struct {
	Revision   string     `json:"revision"`
	State      string     `json:"state"`
	Phase      string     `json:"phase"`
	AccountID  int64      `json:"account_id"`
	Reason     string     `json:"reason"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (s *AccountTestService) StartPersistedAutomaticGatewayBorrow() error {
	if s == nil || s.settingService == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	settings, err := s.settingService.GetAstraRouting(ctx)
	if err != nil {
		return err
	}
	if settings.AutoQuality && settings.CookiePool.Enabled {
		s.StartAstraAutomaticSetup(settings)
	}
	return nil
}

// A save schedules one bounded serial preparation run. Saving again cancels
// the previous run, and revision checks prevent stale work publishing readiness.
func (s *AccountTestService) StartAstraAutomaticSetup(settings config.AstraRoutingSettings) {
	if s.cfg.AstraRouting(context.Background()).Revision != settings.Revision {
		return
	}
	s.astraSetupMu.Lock()
	if s.cfg.AstraRouting(context.Background()).Revision != settings.Revision {
		s.astraSetupMu.Unlock()
		return
	}
	if s.astraSetupCancel != nil {
		s.astraSetupCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.astraSetupCancel = cancel
	state := "queued"
	if !settings.CookiePool.Enabled && !settings.WSSession.Enabled {
		state = "disabled"
	}
	s.astraSetupStatus = AstraSetupStatus{Revision: settings.Revision, State: state, StartedAt: time.Now()}
	s.astraSetupMu.Unlock()
	if state == "disabled" {
		cancel()
		return
	}
	go func() {
		defer cancel()
		for {
			cycle, stop := context.WithTimeout(ctx, 10*time.Minute)
			s.runAstraAutomaticSetup(cycle, stop, settings)
			if !settings.AutoQuality {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Second):
			}
			if s.cfg.AstraRouting(ctx).Revision != settings.Revision {
				return
			}
		}
	}()
}
func (s *AccountTestService) runAstraAutomaticSetup(ctx context.Context, cancel context.CancelFunc, settings config.AstraRoutingSettings) {
	defer cancel()
	set := func(state, phase string, id int64, reason string) {
		s.astraSetupMu.Lock()
		defer s.astraSetupMu.Unlock()
		if s.astraSetupStatus.Revision != settings.Revision {
			return
		}
		s.astraSetupStatus.State = state
		s.astraSetupStatus.Phase = phase
		s.astraSetupStatus.AccountID = id
		s.astraSetupStatus.Reason = reason
		if state == "failed" || state == "ready" {
			now := time.Now()
			s.astraSetupStatus.FinishedAt = &now
		}
	}
	// Share serialization with manually requested tests without discarding a save
	// just because an older, cancelled task has not released its lock yet.
	for !s.astraGatewayActionMu.TryLock() {
		select {
		case <-ctx.Done():
			set("failed", "", 0, "setup_cancelled")
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer s.astraGatewayActionMu.Unlock()
	if ctx.Err() != nil || s.cfg.AstraRouting(ctx).Revision != settings.Revision {
		set("failed", "", 0, "setup_cancelled")
		return
	}
	set("running", "source", 0, "")
	provider, ok := s.httpUpstream.(AstraGatewayRuntimeProvider)
	if !ok {
		set("failed", "source", 0, "runtime_unavailable")
		return
	}
	if !settings.AutoQuality || !borrowSourceStillWarm(provider.AstraGatewaySnapshot(ctx), time.Now()) || borrowAnyModelNeedsWarm(provider.AstraGatewaySnapshot(ctx), settings.CookiePool, time.Now()) {
		if err := prepareAstraForSetup(ctx, provider); err != nil {
			set("failed", "source", 0, astraSetupError(err))
			return
		}
	}
	var firstTargetError error
	var firstFailedTarget int64
	if settings.AutoQuality {
		var wait sync.WaitGroup
		var resultMu sync.Mutex
		slots := make(chan struct{}, 2)
		for _, id := range settings.CookiePool.TargetAccountIDs {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
			}
			if ctx.Err() != nil {
				break
			}
			wait.Add(1)
			go func(id int64) {
				defer wait.Done()
				defer func() { <-slots }()
				for _, model := range settings.CookiePool.ModelsForTarget(id) {
					if !borrowModelNeedsWarm(provider.AstraGatewaySnapshot(ctx), id, model, time.Now()) {
						continue
					}
					err := s.verifyGatewayBorrowTargetForModel(ctx, id, model)
					if err != nil {
						resultMu.Lock()
						if firstTargetError == nil {
							firstTargetError, firstFailedTarget = err, id
						}
						resultMu.Unlock()
					}
				}
			}(id)
		}
		wait.Wait()
	} else {
		for _, id := range settings.CookiePool.TargetAccountIDs {
			if ctx.Err() != nil || s.cfg.AstraRouting(ctx).Revision != settings.Revision {
				set("failed", "target", id, "configuration_changed")
				return
			}
			set("running", "target", id, "")
			// The shared state probe verifies target credentials on the borrowed route.
			var err error
			if settings.AutoQuality {
				for _, model := range settings.CookiePool.ModelsForTarget(id) {
					if !borrowModelNeedsWarm(provider.AstraGatewaySnapshot(ctx), id, model, time.Now()) {
						continue
					}
					if modelErr := s.verifyGatewayBorrowTargetForModel(ctx, id, model); modelErr != nil && err == nil {
						err = modelErr
					}
				}
			} else {
				err = s.verifyAstraGatewayTarget(ctx, id)
			}
			if err != nil && firstTargetError == nil {
				firstTargetError, firstFailedTarget = err, id
			}
		}
	}
	if firstTargetError != nil {
		set("failed", "target", firstFailedTarget, astraSetupError(firstTargetError))
		return
	}
	if settings.CookiePool.Enabled {
		if id, ready := astraTargetsReadyForModels(ctx, provider, settings.CookiePool); !ready {
			set("failed", "target", id, "target_not_verified")
			return
		}
	}
	set("ready", "complete", 0, "")
}

func borrowAnyModelNeedsWarm(snapshot AstraGatewayRuntime, pool config.CodexGatewayPinConfig, now time.Time) bool {
	for _, id := range pool.TargetAccountIDs {
		for _, model := range pool.ModelsForTarget(id) {
			if borrowModelNeedsWarm(snapshot, id, model, now) {
				return true
			}
		}
	}
	return false
}

func borrowModelNeedsWarm(snapshot AstraGatewayRuntime, id int64, model string, now time.Time) bool {
	for _, row := range snapshot.Targets {
		if row.AccountID != id || row.Model != model {
			continue
		}
		if row.RetryAt != nil && now.Before(*row.RetryAt) {
			return false
		}
		return row.State != "ready" || row.ExpiresAt == nil || !now.Add(90*time.Second).Before(*row.ExpiresAt)
	}
	return true
}

func borrowSourceStillWarm(snapshot AstraGatewayRuntime, now time.Time) bool {
	for _, row := range snapshot.Sources {
		if row.ExpiresAt != nil && now.Add(30*time.Second).Before(*row.ExpiresAt) && (row.State == "ready" || row.State == "candidate") {
			return true
		}
	}
	return false
}
func astraSetupError(err error) string {
	switch err.Error() {
	case "astra_rotation_cooling", "astra_rotation_unavailable", "astra_rotation_use_once", "astra_rotation_no_nodes", "astra_rotation_node_unavailable", "astra_rotation_exhausted", "answer_mismatch", "upstream_test_failed", "no_qualified_source_route", "source_probe_cooldown", "configuration_changed", "cookie_pool_disabled", "target_probe_degraded", "target_route_changed", "target_probe_failed", "target_validation_in_progress", "preparation_in_progress":
		return err.Error()
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return "setup_cancelled"
	}
	return "setup_failed"
}

func (s *AccountTestService) StopAstraAutomaticSetup() {
	s.astraSetupMu.Lock()
	defer s.astraSetupMu.Unlock()
	if s.astraSetupCancel != nil {
		s.astraSetupCancel()
	}
}

// A later target may reject and rotate away from an earlier target's route.
// Only report setup success when all selected targets still match current routes.
func astraTargetsReady(ctx context.Context, provider AstraGatewayRuntimeProvider, ids []int64) (int64, bool) {
	return astraTargetsReadyForModels(ctx, provider, config.CodexGatewayPinConfig{TargetAccountIDs: ids})
}

func astraTargetsReadyForModels(ctx context.Context, provider AstraGatewayRuntimeProvider, pool config.CodexGatewayPinConfig) (int64, bool) {
	snapshot := provider.AstraGatewaySnapshot(ctx)
	for _, id := range pool.TargetAccountIDs {
		for _, model := range pool.ModelsForTarget(id) {
			ready := false
			for _, row := range snapshot.Targets {
				if row.AccountID == id && row.State == "ready" && (row.Model == model || (row.Model == "" && model == "gpt-6-astra")) {
					ready = true
					break
				}
			}
			if !ready {
				return id, false
			}
		}
	}
	return 0, true
}

// Saving may overlap an on-demand preparation. Wait for it rather than
// incorrectly publishing a terminal failure while the source is still running.
func prepareAstraForSetup(ctx context.Context, provider AstraGatewayRuntimeProvider) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := provider.PrepareAstraGateway(ctx)
		if err == nil || err.Error() != "preparation_in_progress" {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
