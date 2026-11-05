//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newAutoConfigModelFixture(t *testing.T) (*accountRepository, *service.Account) {
	t.Helper()
	ctx := context.Background()
	repo, account, _ := newBorrowPolicyFixture(t, ctx)
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials||
 '{"plan_type":"prolite","model_mapping":{"old":"old"},"model_mapping_mode":"aliases"}'::jsonb
 WHERE id=$1`, account.ID)
	require.NoError(t, err)
	account, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	return repo, account
}

func TestAutoConfigModelRepositoryPreservesRefreshAndCAS(t *testing.T) {
	for _, tt := range []struct {
		name    string
		sql     string
		applied bool
	}{
		{"matching", "", true},
		{"concurrent token refresh", `UPDATE accounts SET credentials=credentials||'{"access_token":"fresh-token","refresh_token":"fresh-refresh","expires_at":"later"}'::jsonb WHERE id=$1`, true},
		{"unrelated quota", `UPDATE accounts SET extra=extra||'{"codex_7d_used_percent":92}'::jsonb WHERE id=$1`, true},
		{"manual capacity and state", `UPDATE accounts SET concurrency=89,priority=8,schedulable=false,status='error' WHERE id=$1`, true},
		{"changed plan", `UPDATE accounts SET credentials=jsonb_set(credentials,'{plan_type}','"plus"') WHERE id=$1`, false},
		{"changed mapping", `UPDATE accounts SET credentials=jsonb_set(credentials,'{model_mapping}','{"new":"custom"}') WHERE id=$1`, false},
		{"changed mapping mode", `UPDATE accounts SET credentials=jsonb_set(credentials,'{model_mapping_mode}','"whitelist"') WHERE id=$1`, false},
		{"changed owner", `UPDATE accounts SET credentials=jsonb_set(credentials,'{chatgpt_account_id}','"other-owner"') WHERE id=$1`, false},
		{"changed quality state", `UPDATE accounts SET extra=jsonb_set(extra,'{quality_candy,state}','"healthy"') WHERE id=$1`, false},
		{"changed native creation proof", `UPDATE accounts SET extra=extra||'{"openai_account_defaults_identity_fingerprint":"changed"}'::jsonb WHERE id=$1`, false},
		{"changed native policy ownership", `UPDATE accounts SET extra=extra||'{"openai_account_defaults_revision":"native"}'::jsonb WHERE id=$1`, false},
		{"changed BPS", `UPDATE accounts SET extra=jsonb_set(extra,'{openai_excel_bps}','false') WHERE id=$1`, false},
		{"AgentIdentity", `UPDATE accounts SET credentials=credentials||'{"auth_mode":"agentIdentity"}'::jsonb WHERE id=$1`, false},
		{"API key", `UPDATE accounts SET type='apikey' WHERE id=$1`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repo, observed := newAutoConfigModelFixture(t)
			if tt.sql != "" {
				_, err := integrationDB.ExecContext(ctx, tt.sql, observed.ID)
				require.NoError(t, err)
			}
			before := nativeRateLimitAccountRow(t, ctx, repo, observed.ID)
			outbox := nativeRateLimitOutboxCount(t, ctx, repo)
			desired := map[string]string{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6.1-sol"}
			applied, err := repo.ReconcileOAuthAccountModels(ctx, observed, desired)
			require.NoError(t, err)
			require.Equal(t, tt.applied, applied)
			after := nativeRateLimitAccountRow(t, ctx, repo, observed.ID)
			if !tt.applied {
				require.Equal(t, before, after)
				require.Equal(t, outbox, nativeRateLimitOutboxCount(t, ctx, repo))
				return
			}
			raw, err := json.Marshal(after["credentials"].(map[string]any)["model_mapping"])
			require.NoError(t, err)
			require.JSONEq(t, `{"gpt-6-astra":"gpt-6-astra","gpt-6-sol":"gpt-6.1-sol"}`, string(raw))
			require.Equal(t, "aliases", after["credentials"].(map[string]any)["model_mapping_mode"], "do not retroactively widen or narrow mapping mode")
			delete(before["credentials"].(map[string]any), "model_mapping")
			delete(after["credentials"].(map[string]any), "model_mapping")
			before["updated_at"], after["updated_at"] = nil, nil
			require.Equal(t, before, after, "only model_mapping and updated_at may change")
			require.Equal(t, outbox+1, nativeRateLimitOutboxCount(t, ctx, repo))
			again, err := repo.ReconcileOAuthAccountModels(ctx, observed, desired)
			require.NoError(t, err)
			require.False(t, again)
			require.Equal(t, outbox+1, nativeRateLimitOutboxCount(t, ctx, repo))
		})
	}
}

func TestAutoConfigModelRepositoryHonorsCallerRollback(t *testing.T) {
	ctx := context.Background()
	repo, observed := newAutoConfigModelFixture(t)
	before := nativeRateLimitAccountRow(t, ctx, repo, observed.ID)
	outbox := nativeRateLimitOutboxCount(t, ctx, repo)
	tx, err := repo.client.Tx(ctx)
	require.NoError(t, err)
	applied, err := repo.ReconcileOAuthAccountModels(dbent.NewTxContext(ctx, tx), observed, map[string]string{"gpt-6-luna": "gpt-6-luna"})
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, tx.Rollback())
	require.Equal(t, before, nativeRateLimitAccountRow(t, ctx, repo, observed.ID))
	require.Equal(t, outbox, nativeRateLimitOutboxCount(t, ctx, repo))
}
