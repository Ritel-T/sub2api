package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ReconcileOAuthAccountModels atomically changes only model_mapping. A token
// refresh may complete between the observation and this lock without being lost.
func (r *accountRepository) ReconcileOAuthAccountModels(ctx context.Context, observed *service.Account, desired map[string]string) (bool, error) {
	if observed == nil || observed.ID <= 0 {
		return false, nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.reconcileOAuthAccountModelsInTx(ctx, observed, desired)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.reconcileOAuthAccountModelsInTx(ctx, observed, desired)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	applied, err := r.reconcileOAuthAccountModelsInTx(dbent.NewTxContext(ctx, tx), observed, desired)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if applied {
		r.syncSchedulerAccountSnapshotDetached(ctx, observed.ID)
	}
	return applied, nil
}

func (r *accountRepository) reconcileOAuthAccountModelsInTx(ctx context.Context, observed *service.Account, desired map[string]string) (bool, error) {
	client := clientFromContext(ctx, r.client)
	var raw string
	err := scanSingleRow(ctx, client, `SELECT jsonb_build_object(
 'platform',platform,'type',type,'parent_account_id',parent_account_id,
 'credentials',credentials,'extra',extra)::text
 FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, []any{observed.ID}, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var row struct {
		Platform    string         `json:"platform"`
		Type        string         `json:"type"`
		ParentID    *int64         `json:"parent_account_id"`
		Credentials map[string]any `json:"credentials"`
		Extra       map[string]any `json:"extra"`
	}
	if err = json.Unmarshal([]byte(raw), &row); err != nil {
		return false, err
	}
	current := &service.Account{ID: observed.ID, Platform: row.Platform, Type: row.Type,
		ParentAccountID: row.ParentID, Credentials: row.Credentials, Extra: row.Extra}
	if !service.OAuthAccountModelReconcileEligible(current) {
		return false, nil
	}
	before, err := json.Marshal(service.OAuthAccountModelReconcileState(observed))
	if err != nil {
		return false, err
	}
	now, err := json.Marshal(service.OAuthAccountModelReconcileState(current))
	if err != nil {
		return false, err
	}
	if !bytes.Equal(before, now) {
		return false, nil
	}
	mapping, err := json.Marshal(desired)
	if err != nil {
		return false, err
	}
	existing, err := json.Marshal(current.Credentials["model_mapping"])
	if err != nil {
		return false, err
	}
	if bytes.Equal(existing, mapping) {
		return false, nil
	}
	var changed int64
	if err = scanSingleRow(ctx, client, `UPDATE accounts
 SET credentials=jsonb_set(COALESCE(credentials,'{}'::jsonb),'{model_mapping}',$2::jsonb,true),
 updated_at=NOW() WHERE id=$1 RETURNING id`, []any{observed.ID, string(mapping)}, &changed); err != nil {
		return false, err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &observed.ID, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}
