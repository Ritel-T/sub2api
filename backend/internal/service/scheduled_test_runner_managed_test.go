//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledAccountTesterStub struct {
	calls  int
	result *ScheduledTestResult
	err    error
}

func (s *scheduledAccountTesterStub) RunTestBackground(context.Context, int64, string) (*ScheduledTestResult, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	clone := *s.result
	return &clone, nil
}

type managedRecoveryAccountRepoStub struct {
	*managedScheduledTestAccountRepoStub
	clearResult bool
}

func (r *managedRecoveryAccountRepoStub) ClearOpenAIAccountRateLimitIfObserved(context.Context, int64, *time.Time, *time.Time) (bool, error) {
	return r.clearResult, nil
}

func TestScheduledTestRunnerManagedPlanSkipsHealthyAndRecoversObservedBlock(t *testing.T) {
	key := ManagedScheduledTestTemplateOpenAIQuotaRecoveryV1
	template := testManagedScheduledTestTemplate()
	template.Enabled = true
	plan := &ScheduledTestPlan{
		ID:                 55,
		AccountID:          7,
		ModelID:            template.ModelID,
		CronExpression:     "1,31 * * * *",
		Enabled:            true,
		MaxResults:         20,
		AutoRecover:        true,
		ManagedTemplateKey: &key,
		OnlyWhenBlocked:    true,
		RecoverScope:       ScheduledTestRecoverScopeAccountRateLimitOnly,
	}

	t.Run("healthy skip advances without upstream request", func(t *testing.T) {
		account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
		baseRepo := &managedScheduledTestAccountRepoStub{mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{7: account}}}
		accountRepo := &managedRecoveryAccountRepoStub{managedScheduledTestAccountRepoStub: baseRepo, clearResult: true}
		planRepo := &managedScheduledTestPlanRepoStub{template: template, plans: map[int64]*ScheduledTestPlan{55: plan}}
		resultRepo := &managedScheduledTestResultRepoStub{}
		tester := &scheduledAccountTesterStub{result: &ScheduledTestResult{Status: "success"}}
		scheduledSvc := NewScheduledTestService(planRepo, resultRepo, accountRepo)
		runner := NewScheduledTestRunnerService(planRepo, scheduledSvc, tester, NewRateLimitService(accountRepo, nil, nil, nil, nil), accountRepo, nil)

		runner.runOnePlan(context.Background(), plan)
		require.Zero(t, tester.calls)
		require.Zero(t, resultRepo.creates)
		require.Equal(t, 1, planRepo.afterRuns)
		require.Equal(t, int64(1), scheduledSvc.ManagedRuntimeMetrics().SkippedHealthy)
	})

	t.Run("successful probe records narrow recovery", func(t *testing.T) {
		limited := time.Now().UTC().Truncate(time.Second)
		reset := limited.Add(time.Hour)
		account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, RateLimitedAt: &limited, RateLimitResetAt: &reset}
		baseRepo := &managedScheduledTestAccountRepoStub{mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{7: account}}}
		accountRepo := &managedRecoveryAccountRepoStub{managedScheduledTestAccountRepoStub: baseRepo, clearResult: true}
		planRepo := &managedScheduledTestPlanRepoStub{template: template, plans: map[int64]*ScheduledTestPlan{55: plan}}
		resultRepo := &managedScheduledTestResultRepoStub{}
		tester := &scheduledAccountTesterStub{result: &ScheduledTestResult{Status: "success", StartedAt: limited, FinishedAt: limited.Add(time.Second)}}
		scheduledSvc := NewScheduledTestService(planRepo, resultRepo, accountRepo)
		runner := NewScheduledTestRunnerService(planRepo, scheduledSvc, tester, NewRateLimitService(accountRepo, nil, nil, nil, nil), accountRepo, nil)

		runner.runOnePlan(context.Background(), plan)
		require.Equal(t, 1, tester.calls)
		require.Equal(t, 1, resultRepo.creates)
		require.Equal(t, ScheduledTestRecoveryCleared, resultRepo.recoveryStatus)
		require.Equal(t, 1, planRepo.afterRuns)
		metrics := scheduledSvc.ManagedRuntimeMetrics()
		require.Equal(t, int64(1), metrics.Success)
		require.Equal(t, int64(1), metrics.Recovered)
	})
}
