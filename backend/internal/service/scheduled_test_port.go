package service

import (
	"context"
	"time"
)

const (
	ManagedScheduledTestTemplateOpenAIQuotaRecoveryV1 = "openai-quota-recovery-v1"

	ScheduledTestRecoverScopeFull                 = "full"
	ScheduledTestRecoverScopeAccountRateLimitOnly = "account_rate_limit_only"

	ScheduledTestRecoveryCleared       = "cleared"
	ScheduledTestRecoveryConflict      = "conflict"
	ScheduledTestRecoveryNotApplicable = "not_applicable"
	ScheduledTestRecoveryError         = "error"
)

// ScheduledTestPlan represents a scheduled test plan domain model.
type ScheduledTestPlan struct {
	ID                 int64      `json:"id"`
	AccountID          int64      `json:"account_id"`
	ModelID            string     `json:"model_id"`
	CronExpression     string     `json:"cron_expression"`
	Enabled            bool       `json:"enabled"`
	MaxResults         int        `json:"max_results"`
	AutoRecover        bool       `json:"auto_recover"`
	ManagedTemplateKey *string    `json:"managed_template_key,omitempty"`
	OnlyWhenBlocked    bool       `json:"only_when_blocked"`
	RecoverScope       string     `json:"recover_scope"`
	LastRunAt          *time.Time `json:"last_run_at"`
	NextRunAt          *time.Time `json:"next_run_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// ScheduledTestResult represents a single test execution result.
type ScheduledTestResult struct {
	ID             int64     `json:"id"`
	PlanID         int64     `json:"plan_id"`
	Status         string    `json:"status"`
	ResponseText   string    `json:"response_text"`
	ErrorMessage   string    `json:"error_message"`
	RecoveryStatus string    `json:"recovery_status"`
	LatencyMs      int64     `json:"latency_ms"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// ManagedScheduledTestTemplate stores activation and policy for a managed
// scheduled-test family. The default template is seeded disabled so operators
// can preview its impact before the first reconcile activates it.
type ManagedScheduledTestTemplate struct {
	TemplateKey      string    `json:"template_key"`
	Platform         string    `json:"platform"`
	AccountType      string    `json:"account_type"`
	ModelID          string    `json:"model_id"`
	IntervalMinutes  int       `json:"interval_minutes"`
	ShardStartMinute int       `json:"shard_start_minute"`
	ShardCount       int       `json:"shard_count"`
	Enabled          bool      `json:"enabled"`
	AutoRecover      bool      `json:"auto_recover"`
	OnlyWhenBlocked  bool      `json:"only_when_blocked"`
	RecoverScope     string    `json:"recover_scope"`
	MaxResults       int       `json:"max_results"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ScheduledTestPlanRepository defines the data access interface for test plans.
type ScheduledTestPlanRepository interface {
	Create(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	GetByID(ctx context.Context, id int64) (*ScheduledTestPlan, error)
	ListByAccountID(ctx context.Context, accountID int64) ([]*ScheduledTestPlan, error)
	ListByManagedTemplateKey(ctx context.Context, templateKey string) ([]*ScheduledTestPlan, error)
	ListDue(ctx context.Context, now time.Time) ([]*ScheduledTestPlan, error)
	Update(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	Delete(ctx context.Context, id int64) error
	UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error
	GetManagedTemplate(ctx context.Context, templateKey string) (*ManagedScheduledTestTemplate, error)
	UpdateManagedTemplate(ctx context.Context, template *ManagedScheduledTestTemplate) (*ManagedScheduledTestTemplate, error)
	SetManagedTemplateEnabled(ctx context.Context, templateKey string, enabled bool) (*ManagedScheduledTestTemplate, error)
}

// ScheduledTestResultRepository defines the data access interface for test results.
type ScheduledTestResultRepository interface {
	Create(ctx context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error)
	ListByPlanID(ctx context.Context, planID int64, limit int) ([]*ScheduledTestResult, error)
	PruneOldResults(ctx context.Context, planID int64, keepCount int) error
	UpdateRecoveryStatus(ctx context.Context, resultID int64, recoveryStatus string) error
}

type ScheduledAccountTester interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}
