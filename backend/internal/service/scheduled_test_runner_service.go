package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc ScheduledAccountTester
	rateLimitSvc   *RateLimitService
	accountRepo    AccountRepository
	cfg            *config.Config

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewScheduledTestRunnerService creates a new runner.
func NewScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc ScheduledAccountTester,
	rateLimitSvc *RateLimitService,
	accountRepo AccountRepository,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   scheduledSvc,
		accountTestSvc: accountTestSvc,
		rateLimitSvc:   rateLimitSvc,
		accountRepo:    accountRepo,
		cfg:            cfg,
	}
}

// Start begins the cron ticker (every minute).
func (s *ScheduledTestRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}

		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc))
		_, err := c.AddFunc("* * * * *", func() { s.runScheduled() })
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] not started (invalid schedule): %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *ScheduledTestRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] cron stop timed out")
			}
		}
	})
}

func (s *ScheduledTestRunnerService) runScheduled() {
	// Delay 10s so execution lands at ~:10 of each minute instead of :00.
	time.Sleep(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	now := time.Now()
	if template, templateErr := s.planRepo.GetManagedTemplate(ctx, ManagedScheduledTestTemplateOpenAIQuotaRecoveryV1); templateErr == nil && template.Enabled {
		if _, reconcileErr := s.scheduledSvc.ReconcileManagedTemplate(ctx, template.TemplateKey, false); reconcileErr != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] managed reconcile error: %v", reconcileErr)
		}
	}
	plans, err := s.planRepo.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] ListDue error: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}

	logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] found %d due plans", len(plans))

	sem := make(chan struct{}, scheduledTestDefaultMaxWorkers)
	var wg sync.WaitGroup

	for _, plan := range plans {
		sem <- struct{}{}
		wg.Add(1)
		go func(p *ScheduledTestPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOnePlan(ctx, p)
		}(plan)
	}

	wg.Wait()
}

func (s *ScheduledTestRunnerService) runOnePlan(ctx context.Context, plan *ScheduledTestPlan) {
	runAt := time.Now()
	defer s.advancePlan(ctx, plan, runAt)

	var observedLimitedAt *time.Time
	var observedResetAt *time.Time
	if plan.ManagedTemplateKey != nil {
		s.scheduledSvc.recordManagedDue()
		account, skipReason, err := s.evaluateManagedPlan(ctx, plan)
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d managed guard error: %v", plan.ID, err)
			s.scheduledSvc.recordManagedSkip(ManagedScheduledTestSkipUnsupportedAccountType)
			return
		}
		if skipReason != "" {
			s.scheduledSvc.recordManagedSkip(skipReason)
			return
		}
		observedLimitedAt = cloneTimePointer(account.RateLimitedAt)
		observedResetAt = cloneTimePointer(account.RateLimitResetAt)
	}

	result, err := s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d RunTestBackground error: %v", plan.ID, err)
		return
	}

	if err := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d SaveResult error: %v", plan.ID, err)
	}
	if plan.ManagedTemplateKey != nil {
		s.scheduledSvc.recordManagedResult(result.Status == "success")
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover {
		var recoveryStatus string
		if plan.RecoverScope == ScheduledTestRecoverScopeAccountRateLimitOnly {
			recoveryStatus = s.tryRecoverObservedOpenAIRateLimit(ctx, plan, observedLimitedAt, observedResetAt)
		} else {
			recoveryStatus = s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
		}
		result.RecoveryStatus = recoveryStatus
		if plan.ManagedTemplateKey != nil {
			s.scheduledSvc.recordManagedRecovery(recoveryStatus)
		}
		if err := s.scheduledSvc.UpdateResultRecoveryStatus(ctx, result.ID, recoveryStatus); err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d update recovery status error: %v", plan.ID, err)
		}
	}
}

func (s *ScheduledTestRunnerService) advancePlan(ctx context.Context, plan *ScheduledTestPlan, runAt time.Time) {
	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d computeNextRun error: %v", plan.ID, err)
		return
	}

	advanceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.planRepo.UpdateAfterRun(advanceCtx, plan.ID, runAt, nextRun); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d UpdateAfterRun error: %v", plan.ID, err)
	}
}

// tryRecoverAccount attempts to recover an account from recoverable runtime state.
func (s *ScheduledTestRunnerService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) string {
	if s.rateLimitSvc == nil {
		return ScheduledTestRecoveryNotApplicable
	}

	recovery, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover failed: %v", planID, err)
		return ScheduledTestRecoveryError
	}
	if recovery == nil {
		return ScheduledTestRecoveryNotApplicable
	}

	if recovery.ClearedError {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
	if recovery.ClearedError || recovery.ClearedRateLimit {
		return ScheduledTestRecoveryCleared
	}
	return ScheduledTestRecoveryNotApplicable
}

func (s *ScheduledTestRunnerService) tryRecoverObservedOpenAIRateLimit(
	ctx context.Context,
	plan *ScheduledTestPlan,
	observedLimitedAt *time.Time,
	observedResetAt *time.Time,
) string {
	if s.rateLimitSvc == nil {
		return ScheduledTestRecoveryNotApplicable
	}
	status, err := s.rateLimitSvc.RecoverOpenAIAccountRateLimitIfObserved(ctx, plan.AccountID, observedLimitedAt, observedResetAt)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d narrow auto-recover failed: %v", plan.ID, err)
		return ScheduledTestRecoveryError
	}
	return status
}

func (s *ScheduledTestRunnerService) evaluateManagedPlan(ctx context.Context, plan *ScheduledTestPlan) (*Account, string, error) {
	if s.accountRepo == nil {
		return nil, "", fmt.Errorf("account repository is unavailable")
	}
	if plan.ManagedTemplateKey != nil {
		template, err := s.planRepo.GetManagedTemplate(ctx, *plan.ManagedTemplateKey)
		if err != nil {
			return nil, "", err
		}
		if !template.Enabled {
			return nil, ManagedScheduledTestSkipManagedPlanOptOut, nil
		}
	}
	account, err := s.accountRepo.GetByID(ctx, plan.AccountID)
	if err != nil {
		return nil, "", err
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() {
		return account, ManagedScheduledTestSkipUnsupportedAccountType, nil
	}
	if !account.IsActive() {
		return account, ManagedScheduledTestSkipInactiveAccount, nil
	}
	if !account.Schedulable {
		return account, ManagedScheduledTestSkipManualUnschedulable, nil
	}
	if account.IsAutoRecoveryTestDisabled() {
		return account, ManagedScheduledTestSkipManagedPlanOptOut, nil
	}
	if !account.IsModelSupported(plan.ModelID) {
		return account, ManagedScheduledTestSkipMissingTestModel, nil
	}
	if plan.OnlyWhenBlocked && account.RateLimitedAt == nil && (account.RateLimitResetAt == nil || !account.RateLimitResetAt.After(time.Now())) {
		return account, ManagedScheduledTestSkipHealthyNoRuntimeBlock, nil
	}
	return account, "", nil
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
