package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	managedPlanActionCreate    = "create"
	managedPlanActionAdopt     = "adopt"
	managedPlanActionUpdate    = "update"
	managedPlanActionUnchanged = "unchanged"
	managedPlanActionSkipped   = "skipped"
	managedPlanActionFailed    = "failed"

	ManagedScheduledTestSkipHealthyNoRuntimeBlock  = "healthy_no_runtime_block"
	ManagedScheduledTestSkipManualUnschedulable    = "manual_unschedulable"
	ManagedScheduledTestSkipInactiveAccount        = "inactive_account"
	ManagedScheduledTestSkipManagedPlanOptOut      = "managed_plan_opt_out"
	ManagedScheduledTestSkipUnsupportedAccountType = "unsupported_account_type"
	ManagedScheduledTestSkipMissingTestModel       = "missing_test_model"
)

type ManagedScheduledTestChange struct {
	AccountID      int64  `json:"account_id"`
	PlanID         int64  `json:"plan_id,omitempty"`
	Action         string `json:"action"`
	Reason         string `json:"reason,omitempty"`
	CronExpression string `json:"cron_expression,omitempty"`
}

type ManagedScheduledTestReconcileReport struct {
	TemplateKey      string                       `json:"template_key"`
	TemplateEnabled  bool                         `json:"template_enabled"`
	Eligible         int                          `json:"eligible"`
	Create           int                          `json:"create"`
	Adopt            int                          `json:"adopt"`
	Update           int                          `json:"update"`
	Unchanged        int                          `json:"unchanged"`
	OptOut           int                          `json:"opt_out"`
	UnsupportedModel int                          `json:"unsupported_model"`
	Invalid          int                          `json:"invalid"`
	Failed           int                          `json:"failed"`
	Shards           map[string]int               `json:"shards"`
	Changes          []ManagedScheduledTestChange `json:"changes"`
}

type ManagedScheduledTestRuntimeMetricsSnapshot struct {
	Due              int64 `json:"scheduled_test_due"`
	SkippedHealthy   int64 `json:"scheduled_test_skipped_healthy"`
	SkippedOther     int64 `json:"scheduled_test_skipped_other"`
	Success          int64 `json:"scheduled_test_success"`
	Failed           int64 `json:"scheduled_test_failed"`
	Recovered        int64 `json:"scheduled_test_recovered"`
	RecoveryConflict int64 `json:"scheduled_test_recovery_conflict"`
	RecoveryError    int64 `json:"scheduled_test_recovery_error"`
}

type managedScheduledTestRuntimeMetrics struct {
	due              atomic.Int64
	skippedHealthy   atomic.Int64
	skippedOther     atomic.Int64
	success          atomic.Int64
	failed           atomic.Int64
	recovered        atomic.Int64
	recoveryConflict atomic.Int64
	recoveryError    atomic.Int64
}

type ManagedScheduledTestStatus struct {
	Template               *ManagedScheduledTestTemplate              `json:"template"`
	Eligible               int                                        `json:"eligible"`
	ManagedPlans           int                                        `json:"managed_plans"`
	MissingPlans           int                                        `json:"missing_plans"`
	CoveragePercent        float64                                    `json:"managed_test_plan_coverage_pct"`
	DuplicatePlans         int                                        `json:"managed_test_plan_duplicate"`
	DuePlans               int                                        `json:"scheduled_test_due_plans"`
	Shards                 map[string]int                             `json:"shards"`
	LastRunAt              *time.Time                                 `json:"last_run_at"`
	LatestResultSuccess    int                                        `json:"latest_result_success"`
	LatestResultFailed     int                                        `json:"latest_result_failed"`
	LatestRecoveryCleared  int                                        `json:"latest_recovery_cleared"`
	LatestRecoveryConflict int                                        `json:"latest_recovery_conflict"`
	LatestRecoveryError    int                                        `json:"latest_recovery_error"`
	RuntimeMetrics         ManagedScheduledTestRuntimeMetricsSnapshot `json:"runtime_metrics"`
}

type managedPlanDecision struct {
	account *Account
	plan    *ScheduledTestPlan
	desired *ScheduledTestPlan
	action  string
	reason  string
}

func (s *ScheduledTestService) GetManagedTemplate(ctx context.Context, templateKey string) (*ManagedScheduledTestTemplate, error) {
	return s.planRepo.GetManagedTemplate(ctx, templateKey)
}

func (s *ScheduledTestService) PreviewManagedTemplate(ctx context.Context, templateKey string) (*ManagedScheduledTestReconcileReport, error) {
	template, decisions, report, err := s.buildManagedTemplateDecisions(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	report.TemplateEnabled = template.Enabled
	appendManagedDecisionChanges(report, decisions)
	return report, nil
}

// ReconcileManagedTemplate applies the previewed changes. activate enables the
// template first; the runner uses activate=false for its periodic compensation.
func (s *ScheduledTestService) ReconcileManagedTemplate(ctx context.Context, templateKey string, activate bool) (*ManagedScheduledTestReconcileReport, error) {
	if activate {
		if _, err := s.planRepo.SetManagedTemplateEnabled(ctx, templateKey, true); err != nil {
			return nil, err
		}
	}
	template, decisions, report, err := s.buildManagedTemplateDecisions(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	if !template.Enabled {
		return nil, fmt.Errorf("managed scheduled-test template %q is disabled", templateKey)
	}
	report.TemplateEnabled = true

	for _, decision := range decisions {
		change := ManagedScheduledTestChange{
			AccountID: decision.account.ID,
			Action:    decision.action,
			Reason:    decision.reason,
		}
		if decision.plan != nil {
			change.PlanID = decision.plan.ID
		}
		if decision.desired != nil {
			change.CronExpression = decision.desired.CronExpression
		}

		switch decision.action {
		case managedPlanActionCreate:
			created, applyErr := s.CreatePlan(ctx, decision.desired)
			if applyErr == nil {
				change.PlanID = created.ID
			}
			if applyErr != nil {
				change.Action = managedPlanActionFailed
				change.Reason = applyErr.Error()
				report.Failed++
			}
		case managedPlanActionAdopt, managedPlanActionUpdate:
			_, applyErr := s.UpdatePlan(ctx, decision.desired)
			if applyErr != nil {
				change.Action = managedPlanActionFailed
				change.Reason = applyErr.Error()
				report.Failed++
			}
		}
		report.Changes = append(report.Changes, change)
	}
	return report, nil
}

func (s *ScheduledTestService) SetManagedTemplateEnabled(ctx context.Context, templateKey string, enabled bool) (*ManagedScheduledTestTemplate, error) {
	template, err := s.planRepo.SetManagedTemplateEnabled(ctx, templateKey, enabled)
	if err != nil {
		return nil, err
	}
	if enabled {
		if _, err := s.ReconcileManagedTemplate(ctx, templateKey, false); err != nil {
			return nil, err
		}
		return template, nil
	}
	plans, err := s.planRepo.ListByManagedTemplateKey(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if !plan.Enabled {
			continue
		}
		plan.Enabled = false
		if _, err := s.UpdatePlan(ctx, plan); err != nil {
			return nil, err
		}
	}
	return template, nil
}

func (s *ScheduledTestService) UpdateManagedTemplate(ctx context.Context, template *ManagedScheduledTestTemplate) (*ManagedScheduledTestTemplate, error) {
	if err := validateManagedScheduledTestTemplate(template); err != nil {
		return nil, err
	}
	updated, err := s.planRepo.UpdateManagedTemplate(ctx, template)
	if err != nil {
		return nil, err
	}
	if updated.Enabled {
		if _, err := s.ReconcileManagedTemplate(ctx, updated.TemplateKey, false); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (s *ScheduledTestService) SetManagedAccountOptOut(ctx context.Context, templateKey string, accountID int64, disabled bool) (*ManagedScheduledTestReconcileReport, error) {
	template, err := s.planRepo.GetManagedTemplate(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != template.Platform || account.Type != template.AccountType || account.IsShadow() {
		return nil, fmt.Errorf("account is not eligible for managed template %q", templateKey)
	}
	if err := s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{AutoRecoveryTestDisabledExtraKey: disabled}); err != nil {
		return nil, err
	}
	if template.Enabled {
		return s.ReconcileManagedTemplate(ctx, templateKey, false)
	}
	return s.PreviewManagedTemplate(ctx, templateKey)
}

func (s *ScheduledTestService) ManagedTemplateStatus(ctx context.Context, templateKey string) (*ManagedScheduledTestStatus, error) {
	template, err := s.planRepo.GetManagedTemplate(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	preview, err := s.PreviewManagedTemplate(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	plans, err := s.planRepo.ListByManagedTemplateKey(ctx, templateKey)
	if err != nil {
		return nil, err
	}
	duePlans, err := s.planRepo.ListDue(ctx, time.Now())
	if err != nil {
		return nil, err
	}

	status := &ManagedScheduledTestStatus{
		Template:       template,
		Eligible:       preview.Eligible,
		ManagedPlans:   len(plans),
		MissingPlans:   preview.Create + preview.Adopt,
		DuplicatePlans: managedPlanDuplicateCount(plans),
		Shards:         make(map[string]int),
		RuntimeMetrics: s.ManagedRuntimeMetrics(),
	}
	for _, plan := range duePlans {
		if plan.ManagedTemplateKey != nil && *plan.ManagedTemplateKey == templateKey {
			status.DuePlans++
		}
	}
	if status.Eligible > 0 {
		covered := status.Eligible - status.MissingPlans
		if covered < 0 {
			covered = 0
		}
		status.CoveragePercent = float64(covered) * 100 / float64(status.Eligible)
	}
	for _, plan := range plans {
		status.Shards[plan.CronExpression]++
		if plan.LastRunAt != nil && (status.LastRunAt == nil || plan.LastRunAt.After(*status.LastRunAt)) {
			value := *plan.LastRunAt
			status.LastRunAt = &value
		}
		results, listErr := s.resultRepo.ListByPlanID(ctx, plan.ID, 1)
		if listErr != nil {
			return nil, listErr
		}
		if len(results) == 0 {
			continue
		}
		switch results[0].Status {
		case "success":
			status.LatestResultSuccess++
		case "failed":
			status.LatestResultFailed++
		}
		switch results[0].RecoveryStatus {
		case ScheduledTestRecoveryCleared:
			status.LatestRecoveryCleared++
		case ScheduledTestRecoveryConflict:
			status.LatestRecoveryConflict++
		case ScheduledTestRecoveryError:
			status.LatestRecoveryError++
		}
	}
	return status, nil
}

func (s *ScheduledTestService) buildManagedTemplateDecisions(ctx context.Context, templateKey string) (*ManagedScheduledTestTemplate, []managedPlanDecision, *ManagedScheduledTestReconcileReport, error) {
	if s == nil || s.accountRepo == nil {
		return nil, nil, nil, fmt.Errorf("managed scheduled-test account repository is unavailable")
	}
	template, err := s.planRepo.GetManagedTemplate(ctx, templateKey)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := validateManagedScheduledTestTemplate(template); err != nil {
		return nil, nil, nil, err
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, template.Platform)
	if err != nil {
		return nil, nil, nil, err
	}
	managedPlans, err := s.planRepo.ListByManagedTemplateKey(ctx, templateKey)
	if err != nil {
		return nil, nil, nil, err
	}
	managedByAccount := make(map[int64]*ScheduledTestPlan, len(managedPlans))
	for _, plan := range managedPlans {
		managedByAccount[plan.AccountID] = plan
	}

	report := &ManagedScheduledTestReconcileReport{
		TemplateKey: templateKey,
		Shards:      make(map[string]int),
	}
	decisions := make([]managedPlanDecision, 0, len(accounts))
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })

	for index := range accounts {
		account := &accounts[index]
		existingManaged := managedByAccount[account.ID]
		if account.IsShadow() || account.Type != template.AccountType {
			report.Invalid++
			decision := managedDisabledDecision(account, existingManaged, ManagedScheduledTestSkipUnsupportedAccountType)
			if decision.action == managedPlanActionUpdate {
				report.Update++
			}
			decisions = append(decisions, decision)
			continue
		}
		if account.IsAutoRecoveryTestDisabled() {
			report.OptOut++
			decision := managedDisabledDecision(account, existingManaged, ManagedScheduledTestSkipManagedPlanOptOut)
			if decision.action == managedPlanActionUpdate {
				report.Update++
			}
			decisions = append(decisions, decision)
			continue
		}
		if !account.IsModelSupported(template.ModelID) {
			report.UnsupportedModel++
			decision := managedDisabledDecision(account, existingManaged, ManagedScheduledTestSkipMissingTestModel)
			if decision.action == managedPlanActionUpdate {
				report.Update++
			}
			decisions = append(decisions, decision)
			continue
		}

		report.Eligible++
		desired := managedPlanForTemplate(account.ID, template)
		report.Shards[desired.CronExpression]++
		if existingManaged != nil {
			desired.ID = existingManaged.ID
			desired.LastRunAt = existingManaged.LastRunAt
			desired.CreatedAt = existingManaged.CreatedAt
			if managedPlanMatches(existingManaged, desired) {
				report.Unchanged++
				decisions = append(decisions, managedPlanDecision{account: account, plan: existingManaged, desired: desired, action: managedPlanActionUnchanged})
			} else {
				report.Update++
				decisions = append(decisions, managedPlanDecision{account: account, plan: existingManaged, desired: desired, action: managedPlanActionUpdate})
			}
			continue
		}

		plans, listErr := s.planRepo.ListByAccountID(ctx, account.ID)
		if listErr != nil {
			return nil, nil, nil, listErr
		}
		adoptable := adoptableScheduledTestPlan(plans)
		if adoptable != nil {
			desired.ID = adoptable.ID
			desired.LastRunAt = adoptable.LastRunAt
			desired.CreatedAt = adoptable.CreatedAt
			report.Adopt++
			decisions = append(decisions, managedPlanDecision{account: account, plan: adoptable, desired: desired, action: managedPlanActionAdopt})
		} else {
			report.Create++
			decisions = append(decisions, managedPlanDecision{account: account, desired: desired, action: managedPlanActionCreate})
		}
	}
	return template, decisions, report, nil
}

func managedPlanForTemplate(accountID int64, template *ManagedScheduledTestTemplate) *ScheduledTestPlan {
	key := template.TemplateKey
	return &ScheduledTestPlan{
		AccountID:          accountID,
		ModelID:            template.ModelID,
		CronExpression:     stableManagedScheduledTestCron(template, accountID),
		Enabled:            template.Enabled,
		MaxResults:         template.MaxResults,
		AutoRecover:        template.AutoRecover,
		ManagedTemplateKey: &key,
		OnlyWhenBlocked:    template.OnlyWhenBlocked,
		RecoverScope:       template.RecoverScope,
	}
}

func stableManagedScheduledTestCron(template *ManagedScheduledTestTemplate, accountID int64) string {
	sum := sha256.Sum256([]byte(template.TemplateKey + ":" + strconv.FormatInt(accountID, 10)))
	shard := int(binary.BigEndian.Uint64(sum[8:16]) % uint64(template.ShardCount))
	baseMinute := template.ShardStartMinute + shard
	minutes := make([]string, 0, 2)
	for minute := baseMinute; minute < 60; minute += template.IntervalMinutes {
		minutes = append(minutes, strconv.Itoa(minute))
	}
	return strings.Join(minutes, ",") + " * * * *"
}

func validateManagedScheduledTestTemplate(template *ManagedScheduledTestTemplate) error {
	if template == nil || template.TemplateKey == "" {
		return fmt.Errorf("managed scheduled-test template is invalid")
	}
	if strings.TrimSpace(template.Platform) == "" || strings.TrimSpace(template.AccountType) == "" || strings.TrimSpace(template.ModelID) == "" || template.MaxResults <= 0 {
		return fmt.Errorf("managed scheduled-test template %q has invalid account, model, or retention settings", template.TemplateKey)
	}
	if template.IntervalMinutes <= 0 || template.IntervalMinutes >= 60 || template.ShardCount <= 0 || template.ShardStartMinute < 0 || template.ShardStartMinute+template.ShardCount > template.IntervalMinutes {
		return fmt.Errorf("managed scheduled-test template %q has invalid interval or shard range", template.TemplateKey)
	}
	if template.RecoverScope != ScheduledTestRecoverScopeFull && template.RecoverScope != ScheduledTestRecoverScopeAccountRateLimitOnly {
		return fmt.Errorf("managed scheduled-test template %q has invalid recover scope", template.TemplateKey)
	}
	return nil
}

func adoptableScheduledTestPlan(plans []*ScheduledTestPlan) *ScheduledTestPlan {
	var candidate *ScheduledTestPlan
	for _, plan := range plans {
		if plan.ManagedTemplateKey != nil || !plan.Enabled || !plan.AutoRecover {
			continue
		}
		if candidate != nil {
			return nil
		}
		candidate = plan
	}
	return candidate
}

func managedPlanMatches(current, desired *ScheduledTestPlan) bool {
	return current != nil && desired != nil &&
		current.ModelID == desired.ModelID &&
		current.CronExpression == desired.CronExpression &&
		current.Enabled == desired.Enabled &&
		current.MaxResults == desired.MaxResults &&
		current.AutoRecover == desired.AutoRecover &&
		current.OnlyWhenBlocked == desired.OnlyWhenBlocked &&
		current.RecoverScope == desired.RecoverScope &&
		current.ManagedTemplateKey != nil && desired.ManagedTemplateKey != nil &&
		*current.ManagedTemplateKey == *desired.ManagedTemplateKey
}

func managedDisabledDecision(account *Account, plan *ScheduledTestPlan, reason string) managedPlanDecision {
	if plan == nil || !plan.Enabled {
		return managedPlanDecision{account: account, plan: plan, action: managedPlanActionSkipped, reason: reason}
	}
	desired := *plan
	desired.Enabled = false
	return managedPlanDecision{account: account, plan: plan, desired: &desired, action: managedPlanActionUpdate, reason: reason}
}

func appendManagedDecisionChanges(report *ManagedScheduledTestReconcileReport, decisions []managedPlanDecision) {
	for _, decision := range decisions {
		change := ManagedScheduledTestChange{AccountID: decision.account.ID, Action: decision.action, Reason: decision.reason}
		if decision.plan != nil {
			change.PlanID = decision.plan.ID
		}
		if decision.desired != nil {
			change.CronExpression = decision.desired.CronExpression
		}
		report.Changes = append(report.Changes, change)
	}
}

func managedPlanDuplicateCount(plans []*ScheduledTestPlan) int {
	seen := make(map[int64]struct{}, len(plans))
	duplicates := 0
	for _, plan := range plans {
		if _, ok := seen[plan.AccountID]; ok {
			duplicates++
			continue
		}
		seen[plan.AccountID] = struct{}{}
	}
	return duplicates
}

func (s *ScheduledTestService) recordManagedDue() {
	s.managedMetrics.due.Add(1)
}

func (s *ScheduledTestService) recordManagedSkip(reason string) {
	if reason == ManagedScheduledTestSkipHealthyNoRuntimeBlock {
		s.managedMetrics.skippedHealthy.Add(1)
		return
	}
	s.managedMetrics.skippedOther.Add(1)
}

func (s *ScheduledTestService) recordManagedResult(success bool) {
	if success {
		s.managedMetrics.success.Add(1)
		return
	}
	s.managedMetrics.failed.Add(1)
}

func (s *ScheduledTestService) recordManagedRecovery(status string) {
	switch status {
	case ScheduledTestRecoveryCleared:
		s.managedMetrics.recovered.Add(1)
	case ScheduledTestRecoveryConflict:
		s.managedMetrics.recoveryConflict.Add(1)
	case ScheduledTestRecoveryError:
		s.managedMetrics.recoveryError.Add(1)
	}
}

func (s *ScheduledTestService) ManagedRuntimeMetrics() ManagedScheduledTestRuntimeMetricsSnapshot {
	return ManagedScheduledTestRuntimeMetricsSnapshot{
		Due:              s.managedMetrics.due.Load(),
		SkippedHealthy:   s.managedMetrics.skippedHealthy.Load(),
		SkippedOther:     s.managedMetrics.skippedOther.Load(),
		Success:          s.managedMetrics.success.Load(),
		Failed:           s.managedMetrics.failed.Load(),
		Recovered:        s.managedMetrics.recovered.Load(),
		RecoveryConflict: s.managedMetrics.recoveryConflict.Load(),
		RecoveryError:    s.managedMetrics.recoveryError.Load(),
	}
}
