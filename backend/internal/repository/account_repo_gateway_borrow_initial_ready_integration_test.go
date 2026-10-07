//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func newInitialReadyFixture(t *testing.T) (*accountRepository, *service.Account, service.GatewayBorrowInitialReadyObservation) {
	t.Helper()
	ctx := context.Background()
	repo, a, _ := newBorrowPolicyFixture(t, ctx)
	extra, _ := json.Marshal(map[string]any{service.GatewayBorrowAccountQualityModeKey: service.GatewayBorrowAccountQualityMode, service.GatewayBorrowAccountQualityPendingKey: true, service.GatewayBorrowInitialReadyKey: false, "unrelated": true, "codex_7d_used_percent": 71})
	_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET extra=$2::jsonb WHERE id=$1", a.ID, string(extra))
	require.NoError(t, err)
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	o := service.GatewayBorrowInitialReadyObservation{ObservedAt: time.Now().UTC(), ExpectedProxyID: a.ProxyID, CredentialSHA256: service.GatewayBorrowCredentialSHA256(a.Credentials), ExpectedPolicy: service.GatewayBorrowPolicyProjection(a.Extra), ExpectedConfig: service.GatewayBorrowInitialConfiguration(a)}
	return repo, a, o
}

func TestGatewayBorrowInitialReadyRepositoryAtomicCAS(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sql     string
		reason  string
		applied bool
	}{
		{"matching", "", "initial_defaults_ready", true},
		{"paused", `UPDATE accounts SET schedulable=false WHERE id=$1`, "paused_or_inactive", false},
		{"inactive", `UPDATE accounts SET status='error' WHERE id=$1`, "paused_or_inactive", false},
		{"token", `UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','"changed"') WHERE id=$1`, "identity_changed", false},
		{"concurrency", `UPDATE accounts SET concurrency=concurrency+1 WHERE id=$1`, "initial_configuration_changed", false},
		{"priority", `UPDATE accounts SET priority=priority+1 WHERE id=$1`, "initial_configuration_changed", false},
		{"mapping", `UPDATE accounts SET credentials=jsonb_set(credentials,'{model_mapping}','{"gpt-6-luna":"gpt-6-luna"}') WHERE id=$1`, "initial_configuration_changed", false},
		{"mode", `UPDATE accounts SET extra=jsonb_set(extra,'{openai_gateway_borrow_quality_mode}','"legacy"') WHERE id=$1`, "policy_mode_changed", false},
		{"classified", `UPDATE accounts SET extra=jsonb_set(extra,'{openai_gateway_borrow_quality_pending}','false') WHERE id=$1`, "already_classified", false},
		{"quota", `UPDATE accounts SET extra=jsonb_set(extra,'{codex_7d_used_percent}','99') WHERE id=$1`, "initial_defaults_ready", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, a, o := newInitialReadyFixture(t)
			if tc.sql != "" {
				_, err := integrationDB.ExecContext(ctx, tc.sql, a.ID)
				require.NoError(t, err)
			}
			before := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			outbox := nativeRateLimitOutboxCount(t, ctx, repo)
			result, err := repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
			require.NoError(t, err)
			require.Equal(t, tc.applied, result.Applied)
			require.Equal(t, tc.reason, result.Reason)
			after := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			if !tc.applied {
				require.Equal(t, before, after)
				require.Equal(t, outbox, nativeRateLimitOutboxCount(t, ctx, repo))
				return
			}
			current, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, true, current.Extra[service.GatewayBorrowAccountQualityPendingKey], "ready does not authorize business traffic")
			require.True(t, service.GatewayBorrowInitialReadyMatchesCurrent(current))
			aExtra := after["extra"].(map[string]any)
			delete(aExtra, service.GatewayBorrowInitialReadyKey)
			delete(aExtra, service.GatewayBorrowInitialReadyFingerprintKey)
			bExtra := before["extra"].(map[string]any)
			delete(bExtra, service.GatewayBorrowInitialReadyKey)
			require.Equal(t, bExtra, aExtra, "no unrelated extra field is replaced")
			before["extra"], after["extra"] = nil, nil
			before["updated_at"], after["updated_at"] = nil, nil
			require.Equal(t, before, after, "only readiness JSON and updated_at may change")
			require.Equal(t, outbox+1, nativeRateLimitOutboxCount(t, ctx, repo))
			again, err := repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
			require.NoError(t, err)
			require.False(t, again.Applied)
			require.Equal(t, "unchanged", again.Reason)
			require.Equal(t, outbox+1, nativeRateLimitOutboxCount(t, ctx, repo), "idempotent retry cannot add outbox")
		})
	}
}

func TestGatewayBorrowInitialReadyRepositoryRefreshesChangedDefaultsOnlyAfterNewObservation(t *testing.T) {
	ctx := context.Background()
	repo, a, o := newInitialReadyFixture(t)
	result, err := repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.True(t, result.Applied)
	_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET concurrency=concurrency+1 WHERE id=$1", a.ID)
	require.NoError(t, err)
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.False(t, service.GatewayBorrowInitialReadyMatchesCurrent(current))
	result, err = repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.Equal(t, "initial_configuration_changed", result.Reason)
	o.ExpectedConfig = service.GatewayBorrowInitialConfiguration(current)
	o.ExpectedPolicy = service.GatewayBorrowPolicyProjection(current.Extra)
	result, err = repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.True(t, result.Applied)
	current, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.True(t, service.GatewayBorrowInitialReadyMatchesCurrent(current))
}

func TestGatewayBorrowInitialReadyRepositoryStaleAccountEditKeepsManagedClassification(t *testing.T) {
	ctx := context.Background()
	repo, a, o := newInitialReadyFixture(t)
	stale, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	result, err := repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.True(t, result.Applied)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=extra||'{"openai_gateway_borrow_quality_pending":false,"quality_candy":{"state":"healthy"},"openai_gateway_borrow_models":[]}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	stale.Extra["custom"] = "ordinary edit"
	require.NoError(t, repo.Update(ctx, stale))
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, false, current.Extra[service.GatewayBorrowAccountQualityPendingKey])
	require.True(t, service.GatewayBorrowInitialReadyMatchesCurrent(current))
	require.Equal(t, map[string]any{"state": "healthy"}, current.Extra["quality_candy"])
	require.Equal(t, []any{}, current.Extra[service.GatewayBorrowModelsKey])
	require.Equal(t, "ordinary edit", current.Extra["custom"])
}

func TestGatewayBorrowInitialReadyRepositoryRejectsConcurrentConfigurationWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo, a, o := newInitialReadyFixture(t)
	writer, err := testEntClient(t).Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.ExecContext(ctx, "UPDATE accounts SET concurrency=concurrency+1 WHERE id=$1", a.ID)
	require.NoError(t, err)
	done := make(chan struct {
		result service.GatewayBorrowPolicyResult
		err    error
	}, 1)
	go func() {
		r, e := repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
		done <- struct {
			result service.GatewayBorrowPolicyResult
			err    error
		}{r, e}
	}()
	require.Eventually(t, func() bool {
		var blocked int
		err := integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%expected_config%FOR UPDATE%'`).Scan(&blocked)
		require.NoError(t, err)
		return blocked > 0
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	outcome := <-done
	require.NoError(t, outcome.err)
	require.Equal(t, "initial_configuration_changed", outcome.result.Reason)
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, false, current.Extra[service.GatewayBorrowInitialReadyKey])
}

func TestGatewayBorrowInitialReadyAndGroupBindingsShareParentLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo, a, o := newInitialReadyFixture(t)
	client := testEntClient(t)
	group := mustCreateGroup(t, client, &service.Group{Name: "initial-ready-shared-lock", Platform: service.PlatformOpenAI})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_groups WHERE group_id=$1", group.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", group.ID)
	})
	writer, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	var locked int64
	require.NoError(t, scanSingleRow(ctx, writer.Client(), "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", []any{a.ID}, &locked))
	done := make(chan error, 1)
	go func() { done <- repo.BindGroups(ctx, a.ID, []int64{group.ID}) }()
	require.Eventually(t, func() bool {
		var blocked int
		err := integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM accounts WHERE id=%FOR UPDATE%'`).Scan(&blocked)
		require.NoError(t, err)
		return blocked > 0
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	require.NoError(t, <-done)
	current, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{group.ID}, current.GroupIDs)
	result, err := repo.MarkGatewayBorrowInitialReadyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.Equal(t, "initial_configuration_changed", result.Reason, "old empty-binding defaults cannot become ready after the binder")
}
