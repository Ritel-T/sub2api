package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	excelBPSCooldownResetKey  = "openai_excel_bps_rate_limit_reset_at"
	excelBPSCooldownReasonKey = "openai_excel_bps_rate_limit_reason"
)

// ExtendExcelBPSRateLimit returns the stored deadline, which may be later than
// until. A shorter or equal observation preserves both the deadline and reason.
// Account state and its outbox event commit together. When the caller owns the
// transaction, the result belongs to that transaction and the outbox propagates
// it only after the caller commits; uncommitted state never enters the cache.
func (r *accountRepository) ExtendExcelBPSRateLimit(ctx context.Context, accountID int64, until time.Time, reason string) (time.Time, error) {
	if accountID <= 0 {
		return time.Time{}, service.ErrAccountNotFound
	}
	until = until.UTC()
	if until.IsZero() || until.Year() < 1 || until.Year() > 9999 {
		return time.Time{}, errors.New("invalid Excel BPS cooldown deadline")
	}
	// Persist a bounded classification, never upstream messages or payloads.
	if reason != "quota_exhausted" {
		reason = "rate_limited"
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.extendExcelBPSRateLimitInTx(ctx, accountID, until, reason)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.extendExcelBPSRateLimitInTx(ctx, accountID, until, reason)
	}
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, err := r.extendExcelBPSRateLimitInTx(dbent.NewTxContext(ctx, tx), accountID, until, reason)
	if err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}
	// Also repair a stale cache on no-op observations by reading durable state.
	r.syncSchedulerAccountSnapshotDetached(ctx, accountID)
	return stored, nil
}

func (r *accountRepository) extendExcelBPSRateLimitInTx(ctx context.Context, accountID int64, until time.Time, reason string) (time.Time, error) {
	client := clientFromContext(ctx, r.client)
	var previous sql.NullString
	// Serialize extensions before comparing timestamps. Go's RFC3339 parser
	// handles offsets and nanoseconds without PostgreSQL casts on malformed JSON.
	err := scanSingleRow(ctx, client, `SELECT extra ->> 'openai_excel_bps_rate_limit_reset_at'
FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{accountID}, &previous)
	if err != nil {
		return time.Time{}, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	if previous.Valid {
		if stored, err := time.Parse(time.RFC3339Nano, previous.String); err == nil && !until.After(stored) {
			return stored, nil
		}
	}
	var deadline string
	err = scanSingleRow(ctx, client, `UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
  'openai_excel_bps_rate_limit_reset_at', $2::text,
  'openai_excel_bps_rate_limit_reason', $3::text), updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING extra ->> 'openai_excel_bps_rate_limit_reset_at'`,
		[]any{accountID, until.Format(time.RFC3339Nano), reason}, &deadline)
	if err != nil {
		return time.Time{}, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	stored, err := time.Parse(time.RFC3339Nano, deadline)
	if err != nil {
		return time.Time{}, errors.New("invalid persisted Excel BPS cooldown deadline")
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return time.Time{}, err
	}
	return stored, nil
}
