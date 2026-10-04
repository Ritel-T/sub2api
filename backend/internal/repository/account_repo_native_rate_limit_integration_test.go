//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type nativeRateLimitSnapshotRecorder struct {
	service.SchedulerCache
	snapshots []*service.Account
}

func (r *nativeRateLimitSnapshotRecorder) SetAccount(_ context.Context, a *service.Account) error {
	r.snapshots = append(r.snapshots, a)
	return nil
}

func nativeRateLimitOutboxCount(t *testing.T, ctx context.Context, repo *accountRepository) int64 {
	t.Helper()
	rows, err := repo.sql.QueryContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox`)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var count int64
	require.NoError(t, rows.Scan(&count))
	require.NoError(t, rows.Err())
	return count
}

func nativeRateLimitAccountRow(t *testing.T, ctx context.Context, repo *accountRepository, id int64) map[string]any {
	t.Helper()
	rows, err := repo.sql.QueryContext(ctx, `SELECT to_jsonb(a)::text FROM accounts AS a WHERE id=$1`, id)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var raw string
	require.NoError(t, rows.Scan(&raw))
	require.NoError(t, rows.Err())
	var row map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &row))
	return row
}

func TestAccountRepositoryClearNativeRateLimitIfObserved(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(context.Context, *accountRepository, int64) error
		changed bool
	}{
		{"matching observation", nil, false},
		{"later reset", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET rate_limit_reset_at = rate_limit_reset_at + INTERVAL '1 hour' WHERE id=$1`, id)
			return err
		}, true},
		{"new generation same reset", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET rate_limited_at = rate_limited_at + INTERVAL '1 second' WHERE id=$1`, id)
			return err
		}, true},
		{"human pause", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET schedulable=false WHERE id=$1`, id)
			return err
		}, true},
		{"status changed", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET status='error' WHERE id=$1`, id)
			return err
		}, true},
		{"token changed", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials, '{access_token}', '"other-token"') WHERE id=$1`, id)
			return err
		}, true},
		{"proxy changed", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET proxy_id=NULL WHERE id=$1`, id)
			return err
		}, true},
		{"API key", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET type='apikey' WHERE id=$1`, id)
			return err
		}, true},
		{"Grok account", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET platform='grok' WHERE id=$1`, id)
			return err
		}, true},
		{"deleted", func(ctx context.Context, r *accountRepository, id int64) error {
			_, err := r.sql.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW() WHERE id=$1`, id)
			return err
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx := testEntTx(t)
			cache := &nativeRateLimitSnapshotRecorder{}
			repo := newAccountRepositoryWithSQL(tx.Client(), tx, cache)
			proxy := mustCreateProxy(t, tx.Client(), &service.Proxy{Name: "native-recovery-proxy", Protocol: "http", Host: "127.0.0.1", Port: 8080})
			a := mustCreateAccount(t, tx.Client(), &service.Account{
				Name: "native-recovery", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "observed-token", "refresh_token": "keep-refresh"},
				Extra:       map[string]any{"model_rate_limits": map[string]any{"gpt-6-astra": "keep"}, "bps_enabled": true, "openai_403_count": 3},
			})
			limited := time.Date(2026, 10, 2, 16, 0, 0, 0, time.UTC)
			reset := limited.Add(24 * time.Hour)
			until := reset.Add(time.Hour)
			_, err := tx.Client().Account.UpdateOneID(a.ID).SetProxyID(proxy.ID).
				SetRateLimitedAt(limited).SetRateLimitResetAt(reset).
				SetOverloadUntil(until).SetTempUnschedulableUntil(until).
				SetTempUnschedulableReason("keep-protection").SetErrorMessage("keep-diagnostic").Save(ctx)
			require.NoError(t, err)
			observed := service.NativeRateLimitClearObservation{
				RateLimitedAt: limited, RateLimitResetAt: reset, ProxyID: &proxy.ID,
				AccessTokenSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("observed-token"))),
			}
			if tc.mutate != nil {
				require.NoError(t, tc.mutate(ctx, repo, a.ID))
			}
			before := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			outboxBefore := nativeRateLimitOutboxCount(t, ctx, repo)
			cleared, err := repo.ClearNativeRateLimitIfObserved(ctx, a.ID, observed)
			require.NoError(t, err)
			require.Equal(t, !tc.changed, cleared)
			after := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			outboxAfter := nativeRateLimitOutboxCount(t, ctx, repo)
			if tc.changed {
				require.Equal(t, before, after, "a stale or paused account must not mutate any field")
				require.Equal(t, outboxBefore, outboxAfter)
				require.Empty(t, cache.snapshots)
			} else {
				require.Nil(t, after["rate_limited_at"])
				require.Nil(t, after["rate_limit_reset_at"])
				before["rate_limited_at"], before["rate_limit_reset_at"] = nil, nil
				require.Equal(t, before, after, "exactly the two native cooldown fields may change")
				require.Equal(t, outboxBefore+1, outboxAfter)
				require.Len(t, cache.snapshots, 1)
				require.Nil(t, cache.snapshots[0].RateLimitResetAt)
				require.NotNil(t, cache.snapshots[0].TempUnschedulableUntil)
				require.NotNil(t, cache.snapshots[0].OverloadUntil)
				again, err := repo.ClearNativeRateLimitIfObserved(ctx, a.ID, observed)
				require.NoError(t, err)
				require.False(t, again, "retry after success is an idempotent no-op")
				require.Len(t, cache.snapshots, 1)
			}
		})
	}
}

// PostgreSQL rechecks the UPDATE predicates after a concurrent writer releases
// its row lock. A probe that began before a newly committed 429 must lose CAS.
func TestAccountRepositoryClearNativeRateLimitConcurrent429(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testEntClient(t)
	a := mustCreateAccount(t, client, &service.Account{
		Name: "native-concurrent-recovery", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "concurrent-test"},
	})
	// This fixture must be committed for the overlapping connection to see it.
	// Clean only its ID after the writers stop; the shared harness is reused.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, err := integrationDB.ExecContext(cleanupCtx, "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanupCtx, "DELETE FROM accounts WHERE id=$1", a.ID)
		require.NoError(t, err)
		var remaining int
		err = integrationDB.QueryRowContext(cleanupCtx, `SELECT
		 (SELECT COUNT(*) FROM accounts WHERE id=$1) +
		 (SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1)`, a.ID).Scan(&remaining)
		require.NoError(t, err)
		require.Zero(t, remaining, "committed rate-limit fixture must not leak")
	})
	limited := time.Now().UTC().Truncate(time.Microsecond)
	reset := limited.Add(time.Hour)
	_, err := client.Account.UpdateOneID(a.ID).SetRateLimitedAt(limited).SetRateLimitResetAt(reset).Save(ctx)
	require.NoError(t, err)
	tx := testTx(t)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET rate_limited_at=rate_limited_at+INTERVAL '1 second', rate_limit_reset_at=rate_limit_reset_at+INTERVAL '1 hour' WHERE id=$1`, a.ID)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	type result struct {
		cleared bool
		err     error
	}
	done := make(chan result, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("rate-limit writer did not stop before fixture cleanup")
		}
	})
	go func() {
		defer close(finished)
		cleared, err := repo.ClearNativeRateLimitIfObserved(ctx, a.ID, service.NativeRateLimitClearObservation{RateLimitedAt: limited, RateLimitResetAt: reset})
		done <- result{cleared, err}
	}()
	// Verify that the recovery query is actually waiting on the concurrent row.
	require.Eventually(t, func() bool {
		var blocked int
		err := integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%WITH updated AS%'`).Scan(&blocked)
		return err == nil && blocked > 0
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	got := <-done
	require.NoError(t, got.err)
	require.False(t, got.cleared)
	after, err := client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, limited.Add(time.Second), *after.RateLimitedAt)
	require.Equal(t, reset.Add(time.Hour), *after.RateLimitResetAt)
}
