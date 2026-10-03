package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// UpdateCodexUsageSnapshotIfObserved merges only the normalized quota update
// built by the service. PostgreSQL rechecks identity and the exact prior snapshot
// timestamp after a concurrent row writer commits. The outbox event is atomic.
func (r *accountRepository) UpdateCodexUsageSnapshotIfObserved(ctx context.Context, id int64, observed service.CodexProbeUsageObservation, expectedUpdatedAt *string, updates map[string]any) (bool, error) {
	raw, err := json.Marshal(updates)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
        WITH updated AS (
            UPDATE accounts AS a
            SET extra = COALESCE(a.extra, '{}'::jsonb) || $6::jsonb
            WHERE a.id = $1
                AND a.deleted_at IS NULL
                AND a.platform = $2
                AND a.type = $3
                AND a.parent_account_id IS NULL
                AND a.proxy_id IS NOT DISTINCT FROM $4::bigint
                AND COALESCE(a.credentials ->> 'access_token', '') <> ''
                AND encode(sha256(convert_to(COALESCE(a.credentials ->> 'access_token', ''), 'UTF8')), 'hex') = $5
                AND (a.extra ->> 'codex_usage_updated_at') IS NOT DISTINCT FROM $7::text
            RETURNING a.id
        )
        INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
        SELECT $8, updated.id, NULL, NULL FROM updated
    `, id, service.PlatformOpenAI, service.AccountTypeOAuth, observed.ProxyID,
		observed.AccessTokenSHA256, string(raw), expectedUpdatedAt, service.SchedulerOutboxEventAccountChanged)
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
