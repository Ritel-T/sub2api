//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGatewayBorrowPolicyRepositoryMigratesLegacyUnprovenSolFailClosed(t *testing.T) {
	ctx := context.Background()
	repo, a, o := newBorrowPolicyFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=extra||'{"openai_excel_bps_required_models":["gpt-6-sol","gpt-6.1-sol","gpt-6-astra"]}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	latest, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	o.ExpectedPolicy = service.GatewayBorrowPolicyProjection(latest.Extra)
	o.BorrowModels = []string{"gpt-6-astra", "gpt-6.1-sol"}
	o.ModelResults["gpt-6.1-sol"] = service.GatewayBorrowModelResult{State: "inconclusive", Model: "gpt-6.1-sol", ReasoningEffort: "medium", ExpectedAnswer: "21", Total: 1, CheckedAt: time.Now().UTC(), RunID: "20261004T190000Z-sol", Algorithm: service.GatewayBorrowCandyAlgorithm, PromptSHA256: service.GatewayBorrowCandyPromptSHA256}
	applied, err := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	require.Equal(t, "legacy_policy_migrated", applied.Reason)
	after, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, []any{"gpt-6-astra", "gpt-6.1-sol"}, after.Extra[service.GatewayBorrowModelsKey])
	require.NotContains(t, after.Extra[service.GatewayBorrowQualityKey], "gpt-6.1-sol", "incomplete Sol evidence must not invent a classification")
	require.Equal(t, false, after.Extra["openai_excel_bps"])
	require.NotNil(t, after.RateLimitResetAt, "migration cannot clear native cooldown")
}

func newBorrowPolicyFixture(t *testing.T, ctx context.Context) (*accountRepository, *service.Account, service.GatewayBorrowPolicyObservation) {
	t.Helper()
	client := testEntClient(t)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	at := time.Now().UTC().Add(-time.Minute)
	record := map[string]any{"version": 1, "algorithm": service.GatewayBorrowCandyAlgorithm, "prompt_sha256": service.GatewayBorrowCandyPromptSHA256, "state": "degraded", "model": "gpt-6-astra", "reasoning_effort": "medium", "expected_answer": "21", "correct": 0, "total": 4, "checked_at": at.Format(time.RFC3339Nano), "run_id": "20261004T190000Z-old"}
	a := mustCreateAccount(t, client, &service.Account{Name: "borrow-policy-CAS", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "borrow-test-token", "chatgpt_account_id": "borrow-test-account", "refresh_token": "keep"}, Extra: map[string]any{"quality_candy": record, "openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra"}, service.ExcelBPSRequiredGroupIDsKey: []int64{16}, service.ExcelBPSRequiredModelsKey: []string{"gpt-6-astra"}, "unrelated": true, "codex_7d_used_percent": 71, "openai_excel_bps_rate_limit_reset_at": at.Add(time.Hour).Format(time.RFC3339Nano)}})
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, err := integrationDB.ExecContext(cleanup, "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(cleanup, "DELETE FROM accounts WHERE id=$1", a.ID)
		require.NoError(t, err)
	})
	_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET rate_limited_at=$2,rate_limit_reset_at=$3 WHERE id=$1", a.ID, at, at.Add(time.Hour))
	require.NoError(t, err)
	latest, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	o := service.GatewayBorrowPolicyObservation{ObservedAt: time.Now().UTC(), CredentialSHA256: service.GatewayBorrowCredentialSHA256(latest.Credentials), ExpectedProxyID: latest.ProxyID, ExpectedPolicy: service.GatewayBorrowPolicyProjection(latest.Extra), BorrowModels: []string{"gpt-6-astra"}, RetireBPS: true, ModelResults: map[string]service.GatewayBorrowModelResult{"gpt-6-astra": {State: "degraded", Model: "gpt-6-astra", ReasoningEffort: "medium", ExpectedAnswer: "21", Correct: 1, Total: 4, CheckedAt: time.Now().UTC(), RunID: "20261004T190000Z-new", Algorithm: service.GatewayBorrowCandyAlgorithm, PromptSHA256: service.GatewayBorrowCandyPromptSHA256}}}
	return repo, latest, o
}
func TestGatewayBorrowPolicyRepositoryAtomicMergeAndGates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(context.Context, *accountRepository, int64) error
		applied bool
	}{
		{"matching", nil, true},
		{"paused", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET schedulable=false WHERE id=$1", id)
			return e
		}, false},
		{"error status", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET status='error' WHERE id=$1", id)
			return e
		}, false},
		{"changed token", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','\"changed\"') WHERE id=$1", id)
			return e
		}, false},
		{"changed account identity", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET credentials=jsonb_set(credentials,'{chatgpt_account_id}','\"changed\"') WHERE id=$1", id)
			return e
		}, false},
		{"changed policy", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{openai_excel_bps}','false') WHERE id=$1", id)
			return e
		}, false},
		{"concurrent quota", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{codex_7d_used_percent}','99') WHERE id=$1", id)
			return e
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, a, o := newBorrowPolicyFixture(t, ctx)
			cache := &nativeRateLimitSnapshotRecorder{}
			repo.schedulerCache = cache
			if tc.mutate != nil {
				require.NoError(t, tc.mutate(ctx, repo, a.ID))
			}
			before := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			outboxBefore := nativeRateLimitOutboxCount(t, ctx, repo)
			result, err := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
			require.NoError(t, err)
			require.Equal(t, tc.applied, result.Applied)
			after := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			if !tc.applied {
				require.Equal(t, before, after)
				require.Equal(t, outboxBefore, nativeRateLimitOutboxCount(t, ctx, repo))
				require.Empty(t, cache.snapshots)
				return
			}
			afterExtra := after["extra"].(map[string]any)
			require.Equal(t, false, afterExtra["openai_excel_bps"])
			require.Equal(t, []any{}, afterExtra[service.ExcelBPSRequiredGroupIDsKey])
			require.Equal(t, []any{"gpt-6-astra"}, afterExtra[service.GatewayBorrowModelsKey])
			for key, v := range before["extra"].(map[string]any) {
				if key == "quality_candy" || key == "openai_excel_bps" || key == service.ExcelBPSRequiredGroupIDsKey || key == service.ExcelBPSRequiredModelsKey {
					continue
				}
				require.Equal(t, v, afterExtra[key], key)
			}
			before["extra"], after["extra"] = nil, nil
			before["updated_at"], after["updated_at"] = nil, nil
			require.Equal(t, before, after, "only owned JSON and updated_at may change")
			require.Equal(t, outboxBefore+1, nativeRateLimitOutboxCount(t, ctx, repo))
			require.Len(t, cache.snapshots, 1)
			require.Equal(t, []any{"gpt-6-astra"}, cache.snapshots[0].Extra[service.GatewayBorrowModelsKey])
			again, err := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
			require.NoError(t, err)
			require.False(t, again.Applied)
		})
	}
}
func TestGatewayBorrowPolicyRepositoryConcurrentOwnedWriterWins(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo, a, o := newBorrowPolicyFixture(t, ctx)
	writer, err := testEntClient(t).Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{openai_excel_bps}','false') WHERE id=$1", a.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		result, e := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
		if e == nil && (result.Applied || result.Reason != "policy_or_identity_changed") {
			e = fmt.Errorf("unexpected result: %+v", result)
		}
		done <- e
	}()
	// Observe an actual blocked row-lock reader before releasing the first writer.
	require.Eventually(t, func() bool {
		var blocked int
		err := integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%parent_account_id%FOR UPDATE%'`).Scan(&blocked)
		require.NoError(t, err)
		return blocked > 0
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	require.NoError(t, <-done)
	latest, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Nil(t, latest.Extra[service.GatewayBorrowModelsKey])
}
func TestGatewayBorrowPolicyRepositoryOutboxFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	repo, a, o := newBorrowPolicyFixture(t, ctx)
	// A deliberately invalid event constraint is scoped to a disposable outer
	// transaction, proving merge and scheduler event share its rollback boundary.
	tx, err := testEntClient(t).Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	before := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
	_, err = tx.ExecContext(ctx, "ALTER TABLE scheduler_outbox ADD CONSTRAINT gateway_borrow_test_reject CHECK (account_id IS DISTINCT FROM "+fmt.Sprint(a.ID)+") NOT VALID")
	require.NoError(t, err)
	_, err = repo.UpdateGatewayBorrowPolicyIfObserved(dbent.NewTxContext(ctx, tx), a.ID, o)
	require.Error(t, err)
	require.NoError(t, tx.Rollback())
	after := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
	b, _ := json.Marshal(before)
	c, _ := json.Marshal(after)
	require.Equal(t, string(b), string(c))
}

func TestGatewayBorrowAccountQualityRepositoryAtomicPolicyAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(context.Context, *accountRepository, int64) error
		applied bool
		reason  string
	}{
		{"matching", nil, true, "account_quality_applied"},
		{"mode changed", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{openai_gateway_borrow_quality_mode}','\"foreign-mode\"') WHERE id=$1", id)
			return e
		}, false, "policy_mode_changed"},
		{"initial defaults not ready", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET extra=extra||'{\"openai_gateway_borrow_quality_pending\":true,\"openai_gateway_borrow_initial_ready\":false}'::jsonb WHERE id=$1", id)
			return e
		}, false, "initial_defaults_not_ready"},
		{"identity changed", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET credentials=jsonb_set(credentials,'{access_token}','\"changed\"') WHERE id=$1", id)
			return e
		}, false, "policy_or_identity_changed"},
		{"configuration changed after defaults", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET concurrency=concurrency+1 WHERE id=$1", id)
			return e
		}, false, "initial_configuration_changed"},
		{"concurrent native quota", func(ctx context.Context, r *accountRepository, id int64) error {
			_, e := r.sql.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{codex_7d_used_percent}','99') WHERE id=$1", id)
			return e
		}, true, "account_quality_applied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo, a, o := newBorrowPolicyFixture(t, ctx)
			_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=extra||'{"openai_gateway_borrow_quality_mode":"astra_controls_sol_v2","openai_gateway_borrow_quality_pending":true,"openai_gateway_borrow_initial_ready":true}'::jsonb WHERE id=$1`, a.ID)
			require.NoError(t, err)
			before, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			fingerprint := service.GatewayBorrowInitialReadyFingerprint(before)
			_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{openai_gateway_borrow_initial_ready_fingerprint}',to_jsonb($2::text)) WHERE id=$1", a.ID, fingerprint)
			require.NoError(t, err)
			before, err = repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			o.ExpectedPolicy = service.GatewayBorrowPolicyProjection(before.Extra)
			o.PolicyMode = service.GatewayBorrowAccountQualityMode
			o.RetireBPS = false
			o.BorrowModels = []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}
			if tc.mutate != nil {
				require.NoError(t, tc.mutate(ctx, repo, a.ID))
			}
			rowBefore := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			outboxBefore := nativeRateLimitOutboxCount(t, ctx, repo)
			result, err := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
			require.NoError(t, err)
			require.Equal(t, tc.applied, result.Applied)
			require.Equal(t, tc.reason, result.Reason)
			rowAfter := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
			if !tc.applied {
				require.Equal(t, rowBefore, rowAfter)
				require.Equal(t, outboxBefore, nativeRateLimitOutboxCount(t, ctx, repo))
				return
			}
			extra := rowAfter["extra"].(map[string]any)
			require.Equal(t, false, extra[service.GatewayBorrowAccountQualityPendingKey])
			require.Equal(t, []any{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}, extra[service.GatewayBorrowModelsKey])
			require.Equal(t, true, extra["openai_excel_bps"], "without retirement this API preserves independent BPS settings")
			require.Equal(t, rowBefore["rate_limit_reset_at"], rowAfter["rate_limit_reset_at"])
			require.Equal(t, rowBefore["credentials"], rowAfter["credentials"])
			require.Equal(t, outboxBefore+1, nativeRateLimitOutboxCount(t, ctx, repo))
			latest, err := repo.GetByID(ctx, a.ID)
			require.NoError(t, err)
			o.ExpectedPolicy = service.GatewayBorrowPolicyProjection(latest.Extra)
			o.PolicyMode = ""
			oldWriter, err := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
			require.NoError(t, err)
			require.False(t, oldWriter.Applied)
			require.Equal(t, "policy_mode_changed", oldWriter.Reason, "even a fresh old writer cannot split linked policy")
		})
	}
}

func TestGatewayBorrowAccountQualityRepositoryMigrationDoesNotInventSamples(t *testing.T) {
	ctx := context.Background()
	repo, a, o := newBorrowPolicyFixture(t, ctx)
	latest, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	record := latest.Extra["quality_candy"].(map[string]any)
	record["latest_probe_at"] = record["checked_at"]
	record["latest_probe_credential_sha256"] = o.CredentialSHA256
	record["latest_probe_proxy_id"] = latest.ProxyID
	raw, err := json.Marshal(map[string]any{service.GatewayBorrowQualityKey: map[string]any{"gpt-6-astra": record}, service.GatewayBorrowModelsKey: []string{"gpt-6-astra"}, "openai_excel_bps": false, service.ExcelBPSRequiredGroupIDsKey: []int64{}, service.ExcelBPSRequiredModelsKey: []string{}})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET extra=extra||$2::jsonb WHERE id=$1", a.ID, string(raw))
	require.NoError(t, err)
	latest, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	o.ExpectedPolicy = service.GatewayBorrowPolicyProjection(latest.Extra)
	o.PolicyMode = service.GatewayBorrowAccountQualityMode
	o.RetireBPS = false
	o.ModelResults = map[string]service.GatewayBorrowModelResult{}
	o.BorrowModels = []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"}
	result, err := repo.UpdateGatewayBorrowPolicyIfObserved(ctx, a.ID, o)
	require.NoError(t, err)
	require.True(t, result.Applied)
	after, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	afterRecord := after.Extra["quality_candy"].(map[string]any)
	require.Equal(t, record["checked_at"], afterRecord["checked_at"])
	require.Equal(t, record["latest_probe_at"], afterRecord["latest_probe_at"])
	require.Equal(t, record["total"], afterRecord["total"])
	require.NotContains(t, after.Extra[service.GatewayBorrowQualityKey], "gpt-6.1-sol")
}
