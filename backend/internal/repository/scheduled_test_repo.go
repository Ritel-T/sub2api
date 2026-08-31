package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// --- Plan Repository ---

type scheduledTestPlanRepository struct {
	db *sql.DB
}

func NewScheduledTestPlanRepository(db *sql.DB) service.ScheduledTestPlanRepository {
	return &scheduledTestPlanRepository{db: db}
}

func (r *scheduledTestPlanRepository) Create(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_plans (
			account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope, next_run_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		ON CONFLICT (account_id, managed_template_key) WHERE managed_template_key IS NOT NULL
		DO UPDATE SET
			model_id = EXCLUDED.model_id,
			cron_expression = EXCLUDED.cron_expression,
			enabled = EXCLUDED.enabled,
			max_results = EXCLUDED.max_results,
			auto_recover = EXCLUDED.auto_recover,
			only_when_blocked = EXCLUDED.only_when_blocked,
			recover_scope = EXCLUDED.recover_scope,
			next_run_at = EXCLUDED.next_run_at,
			updated_at = NOW()
		RETURNING id, account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope,
			last_run_at, next_run_at, created_at, updated_at
	`, plan.AccountID, plan.ModelID, plan.CronExpression, plan.Enabled, plan.MaxResults, plan.AutoRecover,
		plan.ManagedTemplateKey, plan.OnlyWhenBlocked, normalizeScheduledTestRecoverScope(plan.RecoverScope), plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) GetByID(ctx context.Context, id int64) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope,
			last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans WHERE id = $1
	`, id)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) ListByAccountID(ctx context.Context, accountID int64) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope,
			last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans WHERE account_id = $1
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) ListByManagedTemplateKey(ctx context.Context, templateKey string) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope,
			last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans
		WHERE managed_template_key = $1
		ORDER BY account_id ASC
	`, templateKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) ListDue(ctx context.Context, now time.Time) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope,
			last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans
		WHERE enabled = true AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) Update(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE scheduled_test_plans
		SET model_id = $2, cron_expression = $3, enabled = $4, max_results = $5,
			auto_recover = $6, managed_template_key = $7, only_when_blocked = $8,
			recover_scope = $9, next_run_at = $10, updated_at = NOW()
		WHERE id = $1
		RETURNING id, account_id, model_id, cron_expression, enabled, max_results, auto_recover,
			managed_template_key, only_when_blocked, recover_scope,
			last_run_at, next_run_at, created_at, updated_at
	`, plan.ID, plan.ModelID, plan.CronExpression, plan.Enabled, plan.MaxResults, plan.AutoRecover,
		plan.ManagedTemplateKey, plan.OnlyWhenBlocked, normalizeScheduledTestRecoverScope(plan.RecoverScope), plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_test_plans WHERE id = $1`, id)
	return err
}

func (r *scheduledTestPlanRepository) UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_test_plans SET last_run_at = $2, next_run_at = $3, updated_at = NOW() WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

func (r *scheduledTestPlanRepository) GetManagedTemplate(ctx context.Context, templateKey string) (*service.ManagedScheduledTestTemplate, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT template_key, platform, account_type, model_id, interval_minutes,
			shard_start_minute, shard_count, enabled, auto_recover,
			only_when_blocked, recover_scope, max_results, created_at, updated_at
		FROM managed_scheduled_test_templates
		WHERE template_key = $1
	`, templateKey)
	return scanManagedTemplate(row)
}

func (r *scheduledTestPlanRepository) UpdateManagedTemplate(ctx context.Context, template *service.ManagedScheduledTestTemplate) (*service.ManagedScheduledTestTemplate, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE managed_scheduled_test_templates
		SET model_id = $2,
			interval_minutes = $3,
			shard_start_minute = $4,
			shard_count = $5,
			auto_recover = $6,
			only_when_blocked = $7,
			max_results = $8,
			updated_at = NOW()
		WHERE template_key = $1
		RETURNING template_key, platform, account_type, model_id, interval_minutes,
			shard_start_minute, shard_count, enabled, auto_recover,
			only_when_blocked, recover_scope, max_results, created_at, updated_at
	`, template.TemplateKey, template.ModelID, template.IntervalMinutes, template.ShardStartMinute,
		template.ShardCount, template.AutoRecover, template.OnlyWhenBlocked, template.MaxResults)
	return scanManagedTemplate(row)
}

func (r *scheduledTestPlanRepository) SetManagedTemplateEnabled(ctx context.Context, templateKey string, enabled bool) (*service.ManagedScheduledTestTemplate, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE managed_scheduled_test_templates
		SET enabled = $2, updated_at = NOW()
		WHERE template_key = $1
		RETURNING template_key, platform, account_type, model_id, interval_minutes,
			shard_start_minute, shard_count, enabled, auto_recover,
			only_when_blocked, recover_scope, max_results, created_at, updated_at
	`, templateKey, enabled)
	return scanManagedTemplate(row)
}

// --- Result Repository ---

type scheduledTestResultRepository struct {
	db *sql.DB
}

func NewScheduledTestResultRepository(db *sql.DB) service.ScheduledTestResultRepository {
	return &scheduledTestResultRepository{db: db}
}

func (r *scheduledTestResultRepository) Create(ctx context.Context, result *service.ScheduledTestResult) (*service.ScheduledTestResult, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_results (
			plan_id, status, response_text, error_message, recovery_status,
			latency_ms, started_at, finished_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id, plan_id, status, response_text, error_message, recovery_status,
			latency_ms, started_at, finished_at, created_at
	`, result.PlanID, result.Status, result.ResponseText, result.ErrorMessage, result.RecoveryStatus,
		result.LatencyMs, result.StartedAt, result.FinishedAt)

	out := &service.ScheduledTestResult{}
	if err := row.Scan(
		&out.ID, &out.PlanID, &out.Status, &out.ResponseText, &out.ErrorMessage, &out.RecoveryStatus,
		&out.LatencyMs, &out.StartedAt, &out.FinishedAt, &out.CreatedAt,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *scheduledTestResultRepository) ListByPlanID(ctx context.Context, planID int64, limit int) ([]*service.ScheduledTestResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, status, response_text, error_message, recovery_status,
			latency_ms, started_at, finished_at, created_at
		FROM scheduled_test_results
		WHERE plan_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []*service.ScheduledTestResult
	for rows.Next() {
		r := &service.ScheduledTestResult{}
		if err := rows.Scan(
			&r.ID, &r.PlanID, &r.Status, &r.ResponseText, &r.ErrorMessage, &r.RecoveryStatus,
			&r.LatencyMs, &r.StartedAt, &r.FinishedAt, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (r *scheduledTestResultRepository) UpdateRecoveryStatus(ctx context.Context, resultID int64, recoveryStatus string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_test_results
		SET recovery_status = $2
		WHERE id = $1
	`, resultID, recoveryStatus)
	return err
}

func (r *scheduledTestResultRepository) PruneOldResults(ctx context.Context, planID int64, keepCount int) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM scheduled_test_results
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY plan_id ORDER BY created_at DESC) AS rn
				FROM scheduled_test_results
				WHERE plan_id = $1
			) ranked
			WHERE rn > $2
		)
	`, planID, keepCount)
	return err
}

// --- scan helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanPlan(row scannable) (*service.ScheduledTestPlan, error) {
	p := &service.ScheduledTestPlan{}
	if err := row.Scan(
		&p.ID, &p.AccountID, &p.ModelID, &p.CronExpression, &p.Enabled, &p.MaxResults, &p.AutoRecover,
		&p.ManagedTemplateKey, &p.OnlyWhenBlocked, &p.RecoverScope,
		&p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return p, nil
}

func scanManagedTemplate(row scannable) (*service.ManagedScheduledTestTemplate, error) {
	t := &service.ManagedScheduledTestTemplate{}
	if err := row.Scan(
		&t.TemplateKey, &t.Platform, &t.AccountType, &t.ModelID, &t.IntervalMinutes,
		&t.ShardStartMinute, &t.ShardCount, &t.Enabled, &t.AutoRecover,
		&t.OnlyWhenBlocked, &t.RecoverScope, &t.MaxResults, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return t, nil
}

func normalizeScheduledTestRecoverScope(scope string) string {
	if scope == "" {
		return service.ScheduledTestRecoverScopeFull
	}
	return scope
}

func scanPlans(rows *sql.Rows) ([]*service.ScheduledTestPlan, error) {
	var plans []*service.ScheduledTestPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}
