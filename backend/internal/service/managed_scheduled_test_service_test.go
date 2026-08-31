//go:build unit

package service

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type managedScheduledTestAccountRepoStub struct {
	*mockAccountRepoForGemini
	all []Account
}

func (r *managedScheduledTestAccountRepoStub) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	result := make([]Account, 0, len(r.all))
	for _, account := range r.all {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

type managedScheduledTestPlanRepoStub struct {
	template  *ManagedScheduledTestTemplate
	plans     map[int64]*ScheduledTestPlan
	nextID    int64
	creates   int
	updates   int
	afterRuns int
}

func (r *managedScheduledTestPlanRepoStub) Create(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	r.creates++
	for _, current := range r.plans {
		if current.AccountID == plan.AccountID && current.ManagedTemplateKey != nil && plan.ManagedTemplateKey != nil && *current.ManagedTemplateKey == *plan.ManagedTemplateKey {
			plan.ID = current.ID
			clone := *plan
			r.plans[clone.ID] = &clone
			return &clone, nil
		}
	}
	r.nextID++
	plan.ID = r.nextID
	clone := *plan
	r.plans[clone.ID] = &clone
	return &clone, nil
}

func (r *managedScheduledTestPlanRepoStub) GetByID(_ context.Context, id int64) (*ScheduledTestPlan, error) {
	plan, ok := r.plans[id]
	if !ok {
		return nil, errors.New("not found")
	}
	clone := *plan
	return &clone, nil
}

func (r *managedScheduledTestPlanRepoStub) ListByAccountID(_ context.Context, accountID int64) ([]*ScheduledTestPlan, error) {
	var result []*ScheduledTestPlan
	for _, plan := range r.plans {
		if plan.AccountID == accountID {
			clone := *plan
			result = append(result, &clone)
		}
	}
	return result, nil
}

func (r *managedScheduledTestPlanRepoStub) ListByManagedTemplateKey(_ context.Context, templateKey string) ([]*ScheduledTestPlan, error) {
	var result []*ScheduledTestPlan
	for _, plan := range r.plans {
		if plan.ManagedTemplateKey != nil && *plan.ManagedTemplateKey == templateKey {
			clone := *plan
			result = append(result, &clone)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AccountID < result[j].AccountID })
	return result, nil
}

func (r *managedScheduledTestPlanRepoStub) ListDue(_ context.Context, _ time.Time) ([]*ScheduledTestPlan, error) {
	return nil, nil
}

func (r *managedScheduledTestPlanRepoStub) Update(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	r.updates++
	clone := *plan
	r.plans[clone.ID] = &clone
	return &clone, nil
}

func (r *managedScheduledTestPlanRepoStub) Delete(_ context.Context, id int64) error {
	delete(r.plans, id)
	return nil
}

func (r *managedScheduledTestPlanRepoStub) UpdateAfterRun(_ context.Context, _ int64, _ time.Time, _ time.Time) error {
	r.afterRuns++
	return nil
}

func (r *managedScheduledTestPlanRepoStub) GetManagedTemplate(_ context.Context, templateKey string) (*ManagedScheduledTestTemplate, error) {
	if r.template == nil || r.template.TemplateKey != templateKey {
		return nil, errors.New("template not found")
	}
	clone := *r.template
	return &clone, nil
}

func (r *managedScheduledTestPlanRepoStub) UpdateManagedTemplate(_ context.Context, template *ManagedScheduledTestTemplate) (*ManagedScheduledTestTemplate, error) {
	clone := *template
	r.template = &clone
	return &clone, nil
}

func (r *managedScheduledTestPlanRepoStub) SetManagedTemplateEnabled(_ context.Context, templateKey string, enabled bool) (*ManagedScheduledTestTemplate, error) {
	if r.template == nil || r.template.TemplateKey != templateKey {
		return nil, errors.New("template not found")
	}
	r.template.Enabled = enabled
	clone := *r.template
	return &clone, nil
}

type managedScheduledTestResultRepoStub struct {
	creates        int
	recoveryStatus string
}

func (r *managedScheduledTestResultRepoStub) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	r.creates++
	clone := *result
	clone.ID = 1
	return &clone, nil
}
func (*managedScheduledTestResultRepoStub) ListByPlanID(context.Context, int64, int) ([]*ScheduledTestResult, error) {
	return nil, nil
}
func (*managedScheduledTestResultRepoStub) PruneOldResults(context.Context, int64, int) error {
	return nil
}

func (r *managedScheduledTestResultRepoStub) UpdateRecoveryStatus(_ context.Context, _ int64, status string) error {
	r.recoveryStatus = status
	return nil
}

func testManagedScheduledTestTemplate() *ManagedScheduledTestTemplate {
	return &ManagedScheduledTestTemplate{
		TemplateKey:      ManagedScheduledTestTemplateOpenAIQuotaRecoveryV1,
		Platform:         PlatformOpenAI,
		AccountType:      AccountTypeOAuth,
		ModelID:          "gpt-5.6-terra",
		IntervalMinutes:  30,
		ShardStartMinute: 1,
		ShardCount:       8,
		AutoRecover:      true,
		OnlyWhenBlocked:  true,
		RecoverScope:     ScheduledTestRecoverScopeAccountRateLimitOnly,
		MaxResults:       20,
	}
}

func TestStableManagedScheduledTestCronIsDeterministicAndDistributed(t *testing.T) {
	template := testManagedScheduledTestTemplate()
	counts := make(map[string]int)
	for accountID := int64(1); accountID <= 38; accountID++ {
		first := stableManagedScheduledTestCron(template, accountID)
		second := stableManagedScheduledTestCron(template, accountID)
		require.Equal(t, first, second)
		require.Regexp(t, `^[1-8],3[1-8] \* \* \* \*$`, first)
		counts[first]++
	}
	require.Len(t, counts, 8)
	for _, count := range counts {
		require.LessOrEqual(t, count, 6)
	}
}

func TestManagedScheduledTestPreviewAndReconcileAdoptWithoutDuplicate(t *testing.T) {
	template := testManagedScheduledTestTemplate()
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{AutoRecoveryTestDisabledExtraKey: true}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"}}},
	}
	accountRepo := &managedScheduledTestAccountRepoStub{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{},
		all:                      accounts,
	}
	planRepo := &managedScheduledTestPlanRepoStub{
		template: template,
		plans: map[int64]*ScheduledTestPlan{
			10: {ID: 10, AccountID: 1, ModelID: "gpt-5.6-sol", CronExpression: "*/30 * * * *", Enabled: true, AutoRecover: true, MaxResults: 50, RecoverScope: ScheduledTestRecoverScopeFull},
		},
		nextID: 10,
	}
	svc := NewScheduledTestService(planRepo, &managedScheduledTestResultRepoStub{}, accountRepo)

	preview, err := svc.PreviewManagedTemplate(context.Background(), template.TemplateKey)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Eligible)
	require.Equal(t, 1, preview.Adopt)
	require.Equal(t, 1, preview.OptOut)
	require.Equal(t, 1, preview.UnsupportedModel)
	require.Zero(t, planRepo.creates)
	require.Zero(t, planRepo.updates)

	first, err := svc.ReconcileManagedTemplate(context.Background(), template.TemplateKey, true)
	require.NoError(t, err)
	require.Zero(t, first.Failed)
	require.True(t, template.Enabled)
	require.Zero(t, planRepo.creates)
	require.Equal(t, 1, planRepo.updates)
	managed := planRepo.plans[10]
	require.NotNil(t, managed.ManagedTemplateKey)
	require.Equal(t, template.TemplateKey, *managed.ManagedTemplateKey)
	require.Equal(t, template.ModelID, managed.ModelID)
	require.Equal(t, ScheduledTestRecoverScopeAccountRateLimitOnly, managed.RecoverScope)
	require.Equal(t, 20, managed.MaxResults)

	second, err := svc.ReconcileManagedTemplate(context.Background(), template.TemplateKey, true)
	require.NoError(t, err)
	require.Equal(t, 1, second.Unchanged)
	require.Equal(t, 1, planRepo.updates)
	managedPlans, err := planRepo.ListByManagedTemplateKey(context.Background(), template.TemplateKey)
	require.NoError(t, err)
	require.Len(t, managedPlans, 1)
}

type observedRateLimitClearRepoStub struct {
	*mockAccountRepoForGemini
	cleared bool
	err     error
	limited *time.Time
	reset   *time.Time
}

func (r *observedRateLimitClearRepoStub) ClearOpenAIAccountRateLimitIfObserved(_ context.Context, _ int64, limited, reset *time.Time) (bool, error) {
	r.limited = cloneTimePointer(limited)
	r.reset = cloneTimePointer(reset)
	return r.cleared, r.err
}

type conditionalRuntimeBlockerStub struct {
	clearedID     int64
	observedUntil time.Time
}

func (*conditionalRuntimeBlockerStub) BlockAccountScheduling(*Account, time.Time, string) {}
func (*conditionalRuntimeBlockerStub) ClearAccountSchedulingBlock(int64)                  {}
func (r *conditionalRuntimeBlockerStub) ClearAccountSchedulingBlockIfUntil(accountID int64, observedUntil time.Time) bool {
	r.clearedID = accountID
	r.observedUntil = observedUntil
	return true
}

func TestRecoverOpenAIAccountRateLimitIfObservedPreservesNewGeneration(t *testing.T) {
	limited := time.Now().UTC().Truncate(time.Second)
	reset := limited.Add(30 * time.Minute)

	t.Run("cleared", func(t *testing.T) {
		repo := &observedRateLimitClearRepoStub{mockAccountRepoForGemini: &mockAccountRepoForGemini{}, cleared: true}
		blocker := &conditionalRuntimeBlockerStub{}
		svc := NewRateLimitService(repo, nil, nil, nil, nil)
		svc.SetAccountRuntimeBlocker(blocker)
		status, err := svc.RecoverOpenAIAccountRateLimitIfObserved(context.Background(), 42, &limited, &reset)
		require.NoError(t, err)
		require.Equal(t, ScheduledTestRecoveryCleared, status)
		require.Equal(t, int64(42), blocker.clearedID)
		require.Equal(t, reset, blocker.observedUntil)
	})

	t.Run("conflict", func(t *testing.T) {
		repo := &observedRateLimitClearRepoStub{mockAccountRepoForGemini: &mockAccountRepoForGemini{}, cleared: false}
		blocker := &conditionalRuntimeBlockerStub{}
		svc := NewRateLimitService(repo, nil, nil, nil, nil)
		svc.SetAccountRuntimeBlocker(blocker)
		status, err := svc.RecoverOpenAIAccountRateLimitIfObserved(context.Background(), 42, &limited, &reset)
		require.NoError(t, err)
		require.Equal(t, ScheduledTestRecoveryConflict, status)
		require.Zero(t, blocker.clearedID)
	})
}

func TestEvaluateManagedPlanSkipsHealthyAndManualStates(t *testing.T) {
	now := time.Now()
	key := ManagedScheduledTestTemplateOpenAIQuotaRecoveryV1
	template := testManagedScheduledTestTemplate()
	template.Enabled = true
	planRepo := &managedScheduledTestPlanRepoStub{template: template, plans: map[int64]*ScheduledTestPlan{}}
	plan := &ScheduledTestPlan{ID: 1, AccountID: 7, ModelID: template.ModelID, ManagedTemplateKey: &key, OnlyWhenBlocked: true}

	tests := []struct {
		name    string
		account *Account
		want    string
	}{
		{name: "healthy", account: &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}, want: ManagedScheduledTestSkipHealthyNoRuntimeBlock},
		{name: "manual", account: &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: false, RateLimitResetAt: timePointer(now.Add(time.Hour))}, want: ManagedScheduledTestSkipManualUnschedulable},
		{name: "inactive", account: &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusDisabled, Schedulable: true, RateLimitResetAt: timePointer(now.Add(time.Hour))}, want: ManagedScheduledTestSkipInactiveAccount},
		{name: "blocked", account: &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, RateLimitResetAt: timePointer(now.Add(time.Hour))}, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			accountRepo := &managedScheduledTestAccountRepoStub{mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{7: tc.account}}}
			runner := NewScheduledTestRunnerService(planRepo, NewScheduledTestService(planRepo, &managedScheduledTestResultRepoStub{}, accountRepo), nil, nil, accountRepo, nil)
			_, reason, err := runner.evaluateManagedPlan(context.Background(), plan)
			require.NoError(t, err)
			require.Equal(t, tc.want, reason)
		})
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
