package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const balanceRecoverySnapshotSQL = `SELECT jsonb_build_object(
 'id', id, 'platform', platform, 'type', type, 'status', status,
 'schedulable', schedulable, 'error_message', COALESCE(error_message, ''),
 'updated_at', updated_at, 'credentials', credentials, 'extra', extra,
 'proxy_id', proxy_id, 'parent_account_id', parent_account_id)
FROM accounts WHERE id = $1 AND deleted_at IS NULL`

func (r *accountRepository) GetAccountBalanceRecoverySnapshot(ctx context.Context, id int64) (*service.AccountBalanceRecoverySnapshot, error) {
	return r.balanceRecoverySnapshot(ctx, id, false)
}

func (r *accountRepository) balanceRecoverySnapshot(ctx context.Context, id int64, lock bool) (*service.AccountBalanceRecoverySnapshot, error) {
	if id <= 0 {
		return nil, service.ErrAccountNotFound
	}
	client := clientFromContext(ctx, r.client)
	query := balanceRecoverySnapshotSQL
	if lock {
		query += " FOR UPDATE"
	}
	var raw []byte
	if err := scanSingleRow(ctx, client, query, []any{id}, &raw); err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	var snapshot service.AccountBalanceRecoverySnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	// Manual timer writers lock this same account before inserting/cancelling.
	// A separate statement after acquiring the row lock sees their committed work.
	err := scanSingleRow(ctx, client, `SELECT EXISTS (SELECT 1 FROM account_schedulable_timers WHERE account_id = $1 AND status = 'pending')`, []any{id}, &snapshot.PendingManualTimer)
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *accountRepository) RecoverAccountBalanceIfObserved(ctx context.Context, id int64, req service.AccountBalanceRecoveryRequest) (*service.AccountBalanceRecoveryResult, error) {
	if err := service.ValidateAccountBalanceRecoveryRequest(req); err != nil {
		return nil, err
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.recoverAccountBalanceInTx(ctx, id, req)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.recoverAccountBalanceInTx(ctx, id, req)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := r.recoverAccountBalanceInTx(dbent.NewTxContext(ctx, tx), id, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return result, nil
}

func (r *accountRepository) recoverAccountBalanceInTx(ctx context.Context, id int64, req service.AccountBalanceRecoveryRequest) (*service.AccountBalanceRecoveryResult, error) {
	snapshot, err := r.balanceRecoverySnapshot(ctx, id, true)
	if err != nil {
		return nil, err
	}
	preview, err := service.CheckAccountBalanceRecoveryObservation(snapshot, req, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	client := clientFromContext(ctx, r.client)
	result := &service.AccountBalanceRecoveryResult{AccountID: id, Status: service.StatusActive, Schedulable: *req.TargetSchedulable, IndependentLimitsPreserved: true, FundingEvidence: preview.BalanceEvidence.State}
	if result.FundingEvidence != "fresh_positive" {
		result.FundingEvidence = "owner_confirmed"
	}
	// Intentionally do not call ClearError/RecoverAccountState/SetSchedulable:
	// those broad operations clear independent restrictions or cancel timers.
	err = scanSingleRow(ctx, client, `UPDATE accounts SET status = 'active', error_message = NULL, schedulable = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL RETURNING updated_at`, []any{id, *req.TargetSchedulable}, &result.UpdatedAt)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return nil, err
	}
	return result, nil
}
