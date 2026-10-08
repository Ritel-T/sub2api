package service

import (
	"context"
	"errors"
	"sort"
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
	Ready      int        `json:"ready,omitempty"`
	Pending    int        `json:"pending,omitempty"`
	Blocked    int        `json:"blocked,omitempty"`
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
	settings.CookiePool.QualityMode = settings.QualityMode
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
		if state == "running" && phase == "source" {
			s.astraSetupStatus.StartedAt = time.Now()
			s.astraSetupStatus.FinishedAt = nil
			s.astraSetupStatus.Ready, s.astraSetupStatus.Pending, s.astraSetupStatus.Blocked = 0, 0, 0
		}
		if state == "failed" || state == "ready" || state == "partial" || state == "waiting" {
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
	sourceCtx := ctx
	if settings.AutoQuality {
		snapshot := s.AstraGatewayStatus(ctx)
		plan := automaticBorrowWarmPlan(snapshot, settings.CookiePool, time.Now())
		if len(plan) == 0 {
			s.finishAutomaticBorrowCycle(settings, snapshot, nil, ctx.Err())
			return
		}
		ids := make([]int64, 0, len(plan))
		for _, account := range plan {
			ids = append(ids, account.id)
		}
		sourceCtx = WithAstraSourceTargetDemand(ctx, ids)
	}
	if !settings.AutoQuality || !borrowSourceStillWarm(provider.AstraGatewaySnapshot(ctx), time.Now()) || borrowAnyModelNeedsWarm(s.AstraGatewayStatus(ctx), settings.CookiePool, time.Now()) {
		if err := prepareAstraForSetup(sourceCtx, provider); err != nil {
			if settings.AutoQuality && automaticBorrowSourceWaitingReason(err) != "" {
				s.finishAutomaticBorrowSourceWait(settings, s.AstraGatewayStatus(ctx), automaticBorrowSourceWaitingReason(err), ctx.Err())
			} else {
				set("failed", "source", 0, astraSetupError(err))
			}
			return
		}
	}
	var firstTargetError error
	var firstFailedTarget int64
	if settings.AutoQuality {
		// A complete initial inventory can take longer than one routing Cookie.
		// Return within a bounded target window so the next cycle can renew the
		// routes that succeeded early rather than waiting behind slow failures.
		snapshot := s.AstraGatewayStatus(ctx)
		warmCtx, stopWarm := context.WithTimeout(ctx, automaticBorrowTargetWarmBudget)
		defer stopWarm()
		firstFailedTarget, firstTargetError = runAutomaticBorrowWarmTargets(warmCtx, snapshot, settings.CookiePool, time.Now(), func(callCtx context.Context, id int64, model string) error {
			if s.cfg.AstraRouting(callCtx).Revision != settings.Revision {
				return context.Canceled
			}
			return s.verifyGatewayBorrowTargetForModel(callCtx, id, model)
		})
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
	if settings.AutoQuality {
		s.finishAutomaticBorrowCycle(settings, s.AstraGatewayStatus(ctx), firstTargetError, ctx.Err())
		return
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

// Partial model validation is not a failed system, and cancelled/budget-limited
// preparation is never an IQ verdict or authorization to clear account gates.
func (s *AccountTestService) finishAutomaticBorrowCycle(settings config.AstraRoutingSettings, snapshot AstraGatewayRuntime, targetErr, parentErr error) {
	status := automaticBorrowCycleStatus(snapshot, settings.CookiePool, time.Now(), targetErr, parentErr)
	s.publishAutomaticBorrowCycleStatus(settings, status, "target")
}

// Only known source shortage/cooldown is a wait. Runtime, missing preparer and
// unknown fetch errors keep the existing failed state; no exception creates a pass.
func automaticBorrowSourceWaitingReason(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case "no_qualified_source_route", "source_probe_cooldown", "preparation_in_progress", "source_account_rate_limited", "source_account_unavailable":
		return err.Error()
	}
	return ""
}
func (s *AccountTestService) finishAutomaticBorrowSourceWait(settings config.AstraRoutingSettings, snapshot AstraGatewayRuntime, reason string, parentErr error) {
	status := automaticBorrowCycleStatus(snapshot, settings.CookiePool, time.Now(), errors.New(reason), parentErr)
	if parentErr == nil {
		status.State = "waiting"
		if status.Ready > 0 {
			status.State = "partial"
		}
		status.Reason = reason
	}
	s.publishAutomaticBorrowCycleStatus(settings, status, "source")
}
func (s *AccountTestService) publishAutomaticBorrowCycleStatus(settings config.AstraRoutingSettings, status AstraSetupStatus, phase string) {
	s.astraSetupMu.Lock()
	defer s.astraSetupMu.Unlock()
	if s.astraSetupStatus.Revision != settings.Revision {
		return
	}
	s.astraSetupStatus.State = status.State
	s.astraSetupStatus.Phase = phase
	s.astraSetupStatus.AccountID = 0
	s.astraSetupStatus.Reason = status.Reason
	s.astraSetupStatus.Ready, s.astraSetupStatus.Pending, s.astraSetupStatus.Blocked = status.Ready, status.Pending, status.Blocked
	finished := time.Now()
	s.astraSetupStatus.FinishedAt = &finished
}
func automaticBorrowCycleStatus(snapshot AstraGatewayRuntime, pool config.CodexGatewayPinConfig, now time.Time, targetErr, parentErr error) AstraSetupStatus {
	result := AstraSetupStatus{State: "waiting", Reason: "target_not_verified"}
	for _, id := range pool.TargetAccountIDs {
		for _, model := range pool.ModelsForTarget(id) {
			matched := false
			for _, row := range snapshot.Targets {
				if row.AccountID != id || row.Model != model {
					continue
				}
				matched = true
				switch {
				case row.State == "ready" && row.ExpiresAt != nil && now.Before(*row.ExpiresAt):
					result.Ready++
				case row.State == "blocked" || (row.RetryAt != nil && now.Before(*row.RetryAt)):
					result.Blocked++
				default:
					result.Pending++
				}
				break
			}
			if !matched {
				result.Pending++
			}
		}
	}
	if parentErr != nil {
		result.State = "failed"
		result.Reason = "setup_cancelled"
		return result
	}
	if errors.Is(targetErr, context.DeadlineExceeded) {
		result.Reason = "warm_budget_exhausted"
		return result
	}
	if errors.Is(targetErr, context.Canceled) {
		result.State = "failed"
		result.Reason = "setup_cancelled"
		return result
	}
	if result.Pending == 0 && result.Blocked == 0 && result.Ready > 0 && targetErr == nil {
		result.State = "ready"
		result.Reason = ""
		return result
	}
	if result.Ready > 0 {
		result.State = "partial"
		result.Reason = "target_partially_ready"
	} else if result.Blocked > 0 && result.Pending == 0 {
		result.Reason = "exploration_cooling"
	}
	return result
}

const automaticBorrowTargetWarmBudget = 120 * time.Second

type automaticBorrowWarmModel struct {
	model    string
	priority int
	expires  time.Time
	checked  time.Time
	order    int
}
type automaticBorrowWarmAccount struct {
	id     int64
	models []automaticBorrowWarmModel
	order  int
}

func automaticBorrowWarmLess(a, b automaticBorrowWarmModel) bool {
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	if a.priority < 2 && !a.expires.Equal(b.expires) {
		return a.expires.Before(b.expires)
	}
	if a.priority == 2 && !a.checked.Equal(b.checked) {
		return a.checked.Before(b.checked)
	}
	return a.order < b.order
}

// Plan from one immutable snapshot, never perform network work while sorting.
// A failed/missing witness is not upgraded to previously passed by a deadline.
func automaticBorrowWarmPlan(snapshot AstraGatewayRuntime, pool config.CodexGatewayPinConfig, now time.Time) []automaticBorrowWarmAccount {
	plan := make([]automaticBorrowWarmAccount, 0, len(pool.TargetAccountIDs))
	for accountOrder, id := range pool.TargetAccountIDs {
		account := automaticBorrowWarmAccount{id: id, order: accountOrder}
		models := pool.ModelsForTarget(id)
		if pool.QualityMode == config.GatewayBorrowAccountQualityMode {
			// One account classification warms the shared Astra/Sol proof.
			models = []string{"gpt-6-astra"}
		}
		for modelOrder, model := range models {
			if !borrowModelNeedsWarm(snapshot, id, model, now) {
				continue
			}
			item := automaticBorrowWarmModel{model: model, priority: 2, order: modelOrder}
			for _, row := range snapshot.Targets {
				if row.AccountID != id || row.Model != model {
					continue
				}
				if row.CheckedAt != nil {
					item.checked = *row.CheckedAt
				}
				if row.ExpiresAt != nil {
					item.expires = *row.ExpiresAt
					if row.Reason == "target_probe_passed" {
						item.priority = 1
						if now.Before(item.expires) {
							item.priority = 0
						}
					}
				}
				break
			}
			account.models = append(account.models, item)
		}
		if len(account.models) > 0 {
			sort.SliceStable(account.models, func(i, j int) bool { return automaticBorrowWarmLess(account.models[i], account.models[j]) })
			plan = append(plan, account)
		}
	}
	sort.SliceStable(plan, func(i, j int) bool {
		a, b := plan[i].models[0], plan[j].models[0]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		if a.priority < 2 && !a.expires.Equal(b.expires) {
			return a.expires.Before(b.expires)
		}
		if a.priority == 2 && !a.checked.Equal(b.checked) {
			return a.checked.Before(b.checked)
		}
		return plan[i].order < plan[j].order
	})
	// At most two account probes per cycle. Retry/cold ordering prevents repeated
	// slow initial failures consuming every renewal window.
	if len(plan) > 2 {
		plan = plan[:2]
	}
	return plan
}

func runAutomaticBorrowWarmTargets(ctx context.Context, snapshot AstraGatewayRuntime, pool config.CodexGatewayPinConfig, now time.Time, verify func(context.Context, int64, string) error) (int64, error) {
	plan := automaticBorrowWarmPlan(snapshot, pool, now)
	var wait sync.WaitGroup
	var resultMu sync.Mutex
	var firstID int64
	var firstErr error
	for _, account := range plan {
		wait.Add(1)
		go func(account automaticBorrowWarmAccount) {
			defer wait.Done()
			for _, model := range account.models {
				if ctx.Err() != nil {
					break
				}
				// A queued model may still be under negative cooldown. Compare against
				// the current wall clock, but retain this run's witness ordering.
				if !borrowModelNeedsWarm(snapshot, account.id, model.model, time.Now()) {
					continue
				}
				err := verify(ctx, account.id, model.model)
				if err != nil {
					resultMu.Lock()
					if firstErr == nil {
						firstID, firstErr = account.id, err
					}
					resultMu.Unlock()
				}
			}
		}(account)
	}
	wait.Wait()
	if ctx.Err() != nil {
		return firstID, ctx.Err()
	}
	return firstID, firstErr
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
		if row.State == "blocked" && row.Reason == "target_account_unavailable" {
			return false
		}
		if row.RetryAt != nil && now.Before(*row.RetryAt) {
			return false
		}
		return row.State != "ready" || row.ExpiresAt == nil || !now.Add(120*time.Second).Before(*row.ExpiresAt)
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
	case "target_quality_failed", "target_probe_rate_limited", "target_probe_auth_failed", "target_account_rate_limited", "target_account_unavailable", "borrow_route_expired":
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

// Internal source preparation scope from the already validated warm snapshot.
// Missing scope preserves manual preparation; an empty scope authorizes no work.
type astraSourceTargetDemandKey struct{}

func WithAstraSourceTargetDemand(ctx context.Context, ids []int64) context.Context {
	return context.WithValue(ctx, astraSourceTargetDemandKey{}, append([]int64(nil), ids...))
}

func AstraSourceTargetDemandFromContext(ctx context.Context) ([]int64, bool) {
	ids, scoped := ctx.Value(astraSourceTargetDemandKey{}).([]int64)
	return ids, scoped
}
