//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountRepositoryCodexProbeUsageCompareAndMerge(t *testing.T) {
	for _, mode := range []string{"matching", "paused keeps pause", "changed snapshot", "changed token", "changed proxy", "API key", "other platform", "shadow", "deleted", "missing prior timestamp"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			tx := testEntTx(t)
			cache := &nativeRateLimitSnapshotRecorder{}
			repo := newAccountRepositoryWithSQL(tx.Client(), tx, cache)
			previous := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Format(time.RFC3339)
			observation := service.CodexProbeUsageObservation{
				ObservedAt:        time.Now().UTC().Truncate(time.Second),
				AccessTokenSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("observed-token"))),
			}
			a := mustCreateAccount(t, tx.Client(), &service.Account{
				Name: "codex-probe-usage", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "observed-token", "refresh_token": "keep"},
				Extra: map[string]any{
					"codex_usage_updated_at": previous, "codex_5h_used_percent": 100,
					"bps_enabled": true, "model_rate_limits": map[string]any{"gpt-6-astra": "keep"},
					"quality_degraded": true, "codex_reset_credit_snapshot": map[string]any{"keep": "ticket"},
				},
			})
			limited := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			reset := limited.Add(48 * time.Hour)
			_, err := tx.Client().Account.UpdateOneID(a.ID).SetRateLimitedAt(limited).SetRateLimitResetAt(reset).
				SetOverloadUntil(reset).SetTempUnschedulableUntil(reset).SetTempUnschedulableReason("keep").
				SetErrorMessage("keep-diagnostic").Save(ctx)
			require.NoError(t, err)
			switch mode {
			case "paused keeps pause":
				_, err = tx.Client().Account.UpdateOneID(a.ID).SetSchedulable(false).Save(ctx)
			case "changed snapshot":
				_, err = repo.sql.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{codex_usage_updated_at}',to_jsonb($2::text)) WHERE id=$1`, a.ID, observation.ObservedAt.Format(time.RFC3339))
			case "changed token":
				_, err = repo.sql.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"new-token"') WHERE id=$1`, a.ID)
			case "changed proxy":
				proxy := mustCreateProxy(t, tx.Client(), &service.Proxy{Name: "changed-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8080})
				_, err = tx.Client().Account.UpdateOneID(a.ID).SetProxyID(proxy.ID).Save(ctx)
			case "API key":
				_, err = tx.Client().Account.UpdateOneID(a.ID).SetType(service.AccountTypeAPIKey).Save(ctx)
			case "other platform":
				_, err = tx.Client().Account.UpdateOneID(a.ID).SetPlatform(service.PlatformGrok).Save(ctx)
			case "shadow":
				parent := mustCreateAccount(t, tx.Client(), &service.Account{Name: "parent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
				_, err = tx.Client().Account.UpdateOneID(a.ID).SetParentAccountID(parent.ID).SetQuotaDimension("spark").Save(ctx)
			case "deleted":
				_, err = tx.Client().Account.UpdateOneID(a.ID).SetDeletedAt(time.Now()).Save(ctx)
			case "missing prior timestamp":
				_, err = repo.sql.ExecContext(ctx, `UPDATE accounts SET extra=extra - 'codex_usage_updated_at' WHERE id=$1`, a.ID)
			}
			require.NoError(t, err)
			before := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			outboxBefore := nativeRateLimitOutboxCount(t, ctx, repo)
			updates := map[string]any{"codex_usage_updated_at": observation.ObservedAt.Format(time.RFC3339), "codex_5h_used_percent": float64(2)}
			expected := &previous
			if mode == "missing prior timestamp" {
				expected = nil
			}
			updated, err := repo.UpdateCodexUsageSnapshotIfObserved(ctx, a.ID, observation, expected, updates)
			require.NoError(t, err)
			shouldUpdate := mode == "matching" || mode == "paused keeps pause" || mode == "missing prior timestamp"
			require.Equal(t, shouldUpdate, updated)
			after := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			if !shouldUpdate {
				require.Equal(t, before, after)
				require.Equal(t, outboxBefore, nativeRateLimitOutboxCount(t, ctx, repo))
				require.Empty(t, cache.snapshots)
				return
			}
			beforeExtra, ok := before["extra"].(map[string]any)
			require.True(t, ok)
			for key, value := range updates {
				beforeExtra[key] = value
			}
			require.Equal(t, before, after, "only supplied quota extras change; all scheduling and credentials remain intact")
			require.Equal(t, outboxBefore+1, nativeRateLimitOutboxCount(t, ctx, repo))
			require.Len(t, cache.snapshots, 1)
			require.Equal(t, float64(2), cache.snapshots[0].Extra["codex_5h_used_percent"])
			require.True(t, cache.snapshots[0].RateLimitResetAt.Equal(reset))
			if mode == "paused keeps pause" {
				require.False(t, cache.snapshots[0].Schedulable)
			}
			repeated, err := repo.UpdateCodexUsageSnapshotIfObserved(ctx, a.ID, observation, expected, updates)
			require.NoError(t, err)
			require.False(t, repeated)
		})
	}
}

// Real overlapping PostgreSQL updates exercise row-lock predicate rechecks and
// merge semantics. A newer normal-response snapshot wins; unrelated BPS changes
// are preserved while the quota update can still commit.
func TestAccountRepositoryCodexProbeUsageConcurrentWriter(t *testing.T) {
	for _, mode := range []string{"newer normal response", "unrelated BPS field"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := testEntClient(t)
			previous := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Format(time.RFC3339)
			observed := time.Now().UTC().Truncate(time.Second)
			a := mustCreateAccount(t, client, &service.Account{
				Name: "codex-probe-usage-concurrent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "observed-token"},
				Extra:       map[string]any{"codex_usage_updated_at": previous, "codex_5h_used_percent": float64(100), "bps_enabled": true},
			})
			tx := testTx(t)
			var err error
			if mode == "newer normal response" {
				_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=extra || jsonb_build_object('codex_usage_updated_at',$2::text,'codex_5h_used_percent',3,'bps_enabled',false) WHERE id=$1`, a.ID, observed.Add(time.Second).Format(time.RFC3339))
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=extra || '{"bps_enabled":false}'::jsonb WHERE id=$1`, a.ID)
			}
			require.NoError(t, err)
			repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
			type result struct {
				updated bool
				err     error
			}
			done := make(chan result, 1)
			go func() {
				updated, err := repo.UpdateCodexUsageSnapshotIfObserved(ctx, a.ID, service.CodexProbeUsageObservation{
					ObservedAt: observed, AccessTokenSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("observed-token"))),
				}, &previous, map[string]any{"codex_usage_updated_at": observed.Format(time.RFC3339), "codex_5h_used_percent": float64(2)})
				done <- result{updated, err}
			}()
			require.Eventually(t, func() bool {
				var blocked int
				err := integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%WITH updated AS%'`).Scan(&blocked)
				return err == nil && blocked > 0
			}, 2*time.Second, 10*time.Millisecond)
			require.NoError(t, tx.Commit())
			got := <-done
			require.NoError(t, got.err)
			after, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, false, after.Extra["bps_enabled"], "never roll back a concurrent unrelated extra field")
			if mode == "newer normal response" {
				require.False(t, got.updated)
				require.Equal(t, float64(3), after.Extra["codex_5h_used_percent"])
				require.Equal(t, observed.Add(time.Second).Format(time.RFC3339), after.Extra["codex_usage_updated_at"])
			} else {
				require.True(t, got.updated)
				require.Equal(t, float64(2), after.Extra["codex_5h_used_percent"])
				require.Equal(t, observed.Format(time.RFC3339), after.Extra["codex_usage_updated_at"])
			}
		})
	}
}
