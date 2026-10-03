package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ClearNativeRateLimitIfObserved clears a successful OpenAI OAuth probe's
// observed native cooldown. The UPDATE predicates atomically reject a newer
// cooldown, a human pause, deletion, or a changed supplied identity. Its outbox
// event commits in the same SQL statement, and no other protection is cleared.
func (r *accountRepository) ClearNativeRateLimitIfObserved(ctx context.Context, id int64, observed service.NativeRateLimitClearObservation) (bool, error) {
	result, err := r.sql.ExecContext(ctx, `
        WITH updated AS (
            UPDATE accounts AS a
            SET rate_limited_at = NULL,
                rate_limit_reset_at = NULL
            WHERE a.id = $1
                AND a.deleted_at IS NULL
                AND a.platform = $2
                AND a.type = $3
                AND a.status = $4
                AND a.schedulable IS TRUE
                AND a.rate_limited_at = $5::timestamptz
                AND a.rate_limit_reset_at = $6::timestamptz
                AND a.proxy_id IS NOT DISTINCT FROM $7::bigint
                AND ($8::text = '' OR encode(sha256(convert_to(COALESCE(a.credentials ->> 'access_token', ''), 'UTF8')), 'hex') = $8)
            RETURNING a.id
        )
        INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
        SELECT $9, updated.id, NULL, NULL FROM updated
    `, id, service.PlatformOpenAI, service.AccountTypeOAuth, service.StatusActive,
		observed.RateLimitedAt, observed.RateLimitResetAt,
		observed.ProxyID, observed.AccessTokenSHA256, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
