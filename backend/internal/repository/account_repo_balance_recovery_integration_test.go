//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newBalanceRecoveryIntegrationAccount(t *testing.T) (*accountRepository, int64) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	a := mustCreateAccount(t, client, &service.Account{Name: t.Name(), Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusError, ErrorMessage: "Payment required (402): insufficient balance", Credentials: map[string]any{"api_key": "isolated-fake-key"}, Extra: map[string]any{"quality_state": "keep", "model_rate_limits": map[string]any{"test-model": "keep"}, "manual_annotation": "keep"}})
	until := time.Now().UTC().Truncate(time.Second).Add(24 * time.Hour)
	_, err := client.Account.UpdateOneID(a.ID).SetSchedulable(false).SetRateLimitedAt(until.Add(-time.Hour)).SetRateLimitResetAt(until).SetOverloadUntil(until).SetTempUnschedulableUntil(until).SetTempUnschedulableReason("independent").Save(ctx)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM account_schedulable_timers WHERE account_id=$1", a.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
		require.NoError(t, err)
		require.NoError(t, client.Account.DeleteOneID(a.ID).Exec(ctx))
	})
	return newAccountRepositoryWithSQL(client, integrationDB, nil), a.ID
}
func balanceRecoveryIntegrationRequest(t *testing.T, r *accountRepository, id int64, target bool) service.AccountBalanceRecoveryRequest {
	t.Helper()
	snapshot, err := r.GetAccountBalanceRecoverySnapshot(context.Background(), id)
	require.NoError(t, err)
	p, err := service.BuildAccountBalanceRecoveryPreview(snapshot, time.Now().UTC())
	require.NoError(t, err)
	require.True(t, p.Eligible, p.Reason)
	return service.AccountBalanceRecoveryRequest{ExpectedUpdatedAt: p.ExpectedUpdatedAt, ExpectedFingerprint: p.ExpectedFingerprint, TargetSchedulable: &target, FundingConfirmed: true, Confirm: true}
}
func balanceRecoveryIntegrationRow(t *testing.T, id int64, omitRecovery bool) string {
	t.Helper()
	query := "SELECT to_jsonb(a)::text FROM accounts a WHERE id=$1"
	if omitRecovery {
		query = "SELECT (to_jsonb(a)-ARRAY['status','error_message','schedulable','updated_at'])::text FROM accounts a WHERE id=$1"
	}
	var row string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), query, id).Scan(&row))
	return row
}
func balanceRecoveryIntegrationOutbox(t *testing.T, id int64) int {
	t.Helper()
	var n int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", id).Scan(&n))
	return n
}
func TestBalanceRecoveryPostgresPreservesIndependentColumnsAndTimer(t *testing.T) {
	for _, target := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain false", true: "explicit enable"}[target], func(t *testing.T) {
			r, id := newBalanceRecoveryIntegrationAccount(t)
			ctx := context.Background()
			until := time.Now().Add(time.Hour)
			_, err := integrationDB.ExecContext(ctx, `INSERT INTO account_schedulable_timers(account_id,action,version,execute_at,timezone,requested_by,status,next_attempt_at) VALUES($1,'close',1,$2,'UTC',1,'cancelled',$2)`, id, until)
			require.NoError(t, err)
			before := balanceRecoveryIntegrationRow(t, id, true)
			var timerBefore string
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT to_jsonb(t)::text FROM account_schedulable_timers t WHERE account_id=$1", id).Scan(&timerBefore))
			req := balanceRecoveryIntegrationRequest(t, r, id, target)
			got, err := r.RecoverAccountBalanceIfObserved(ctx, id, req)
			require.NoError(t, err)
			require.Equal(t, target, got.Schedulable)
			require.True(t, got.IndependentLimitsPreserved)
			require.Equal(t, before, balanceRecoveryIntegrationRow(t, id, true))
			a, err := r.GetByID(ctx, id)
			require.NoError(t, err)
			require.Equal(t, service.StatusActive, a.Status)
			require.Empty(t, a.ErrorMessage)
			require.Equal(t, target, a.Schedulable)
			require.Equal(t, 1, balanceRecoveryIntegrationOutbox(t, id))
			var timerAfter string
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT to_jsonb(t)::text FROM account_schedulable_timers t WHERE account_id=$1", id).Scan(&timerAfter))
			require.Equal(t, timerBefore, timerAfter)
		})
	}
}
func TestBalanceRecoveryPostgresOuterRollbackIsAtomic(t *testing.T) {
	r, id := newBalanceRecoveryIntegrationAccount(t)
	ctx := context.Background()
	before := balanceRecoveryIntegrationRow(t, id, false)
	req := balanceRecoveryIntegrationRequest(t, r, id, true)
	tx, err := r.client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	txctx := dbent.NewTxContext(ctx, tx)
	got, err := r.RecoverAccountBalanceIfObserved(txctx, id, req)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, got.Status)
	var inTxStatus string
	require.NoError(t, scanSingleRow(txctx, tx.Client(), "SELECT status FROM accounts WHERE id=$1", []any{id}, &inTxStatus))
	require.Equal(t, service.StatusActive, inTxStatus)
	require.Equal(t, before, balanceRecoveryIntegrationRow(t, id, false))
	require.Zero(t, balanceRecoveryIntegrationOutbox(t, id))
	require.NoError(t, tx.Rollback())
	require.Equal(t, before, balanceRecoveryIntegrationRow(t, id, false))
	require.Zero(t, balanceRecoveryIntegrationOutbox(t, id))
}

func waitBalanceRecoveryBlockedBy(t *testing.T, ctx context.Context, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked))
		if blocked {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("recovery did not reach the account row lock")
}
func TestBalanceRecoveryPostgresRejectsConcurrentTimerAndCredentialChange(t *testing.T) {
	for _, change := range []string{"pending timer", "credentials"} {
		t.Run(change, func(t *testing.T) {
			r, id := newBalanceRecoveryIntegrationAccount(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			req := balanceRecoveryIntegrationRequest(t, r, id, true)
			manual, err := integrationDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer manual.Rollback()
			var lockedID int64
			require.NoError(t, manual.QueryRowContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", id).Scan(&lockedID))
			var pid int
			require.NoError(t, manual.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid))
			if change == "pending timer" {
				_, err = manual.ExecContext(ctx, `INSERT INTO account_schedulable_timers(account_id,action,version,execute_at,timezone,requested_by,status,next_attempt_at) VALUES($1,'open',1,$2,'UTC',1,'pending',$2)`, id, time.Now().Add(time.Hour))
			} else {
				_, err = manual.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"api_key":"rotated-isolated-key"}'::jsonb,updated_at=NOW() WHERE id=$1`, id)
			}
			require.NoError(t, err)
			type outcome struct {
				result *service.AccountBalanceRecoveryResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() { got, e := r.RecoverAccountBalanceIfObserved(ctx, id, req); done <- outcome{got, e} }()
			waitBalanceRecoveryBlockedBy(t, ctx, pid)
			require.NoError(t, manual.Commit())
			out := <-done
			require.Nil(t, out.result)
			require.Equal(t, 409, infraerrors.Code(out.err))
			require.Equal(t, "BALANCE_RECOVERY_CHANGED", infraerrors.Reason(out.err))
			a, err := r.GetByID(ctx, id)
			require.NoError(t, err)
			require.Equal(t, service.StatusError, a.Status)
			require.False(t, a.Schedulable)
			require.Zero(t, balanceRecoveryIntegrationOutbox(t, id))
			if change == "pending timer" {
				var status string
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM account_schedulable_timers WHERE account_id=$1", id).Scan(&status))
				require.Equal(t, "pending", status)
			}
		})
	}
}
func TestOpsClientStatusPostgresListAndDetail(t *testing.T) {
	ctx := context.Background()
	r := NewOpsRepository(integrationDB)
	model := t.Name()
	for i, status := range []any{200, 502, nil} {
		var id int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO ops_error_logs(error_phase,error_type,error_owner,error_source,severity,status_code,upstream_status_code,platform,model,created_at) VALUES('upstream','upstream_error','provider','upstream_http','error',$1,402,'anthropic',$2,NOW()) RETURNING id`, status, model).Scan(&id))
		t.Cleanup(func() {
			_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE id=$1", id)
			require.NoError(t, err)
		})
		d, err := r.GetErrorLogByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, 402, d.StatusCode)
		if i == 2 {
			require.Nil(t, d.ClientStatusCode)
		} else {
			require.Equal(t, status, *d.ClientStatusCode)
		}
	}
	logs, err := r.ListErrorLogs(ctx, &service.OpsErrorLogFilter{Model: model, View: "all", IncludeRecoveredUpstream: true, ErrorPhasesAny: []string{"upstream"}})
	require.NoError(t, err)
	require.Len(t, logs.Errors, 3)
	seen := map[int]int{}
	nulls := 0
	for _, item := range logs.Errors {
		require.Equal(t, 402, item.StatusCode)
		if item.ClientStatusCode == nil {
			nulls++
		} else {
			seen[*item.ClientStatusCode]++
		}
	}
	require.Equal(t, 1, nulls)
	require.Equal(t, map[int]int{200: 1, 502: 1}, seen)
}
