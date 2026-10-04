//go:build integration

package repository

import (
	"context"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func poolGroupRow(t *testing.T, ctx context.Context, repo *groupRepository, id int64) string {
	t.Helper()
	var raw string
	require.NoError(t, scanSingleRow(ctx, repo.sql, "SELECT to_jsonb(g)::text FROM groups g WHERE id=$1", []any{id}, &raw))
	return raw
}
func poolBindingsRow(t *testing.T, ctx context.Context, repo *groupRepository, id int64) string {
	t.Helper()
	var raw string
	require.NoError(t, scanSingleRow(ctx, repo.sql, "SELECT COALESCE(jsonb_agg(to_jsonb(b) ORDER BY account_id),'[]'::jsonb)::text FROM account_groups b WHERE group_id=$1", []any{id}, &raw))
	return raw
}
func TestShareAccountPoolPreservesBindingsAndBusinessFields(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newGroupRepositoryWithSQL(client, tx)
	source := mustCreateGroup(t, client, &service.Group{Name: "share-source", Platform: service.PlatformOpenAI, RateMultiplier: 1, Status: service.StatusActive})
	target := mustCreateGroup(t, client, &service.Group{Name: "share-target", Platform: service.PlatformOpenAI, RateMultiplier: .2, Status: service.StatusActive})
	a := mustCreateAccount(t, client, &service.Account{Name: "pool-a", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "test"}, Extra: map[string]any{"keep": "a"}})
	b := mustCreateAccount(t, client, &service.Account{Name: "pool-b", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-b"}})
	_, err := client.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id,priority,allowed_models) VALUES($1,$3,7,'["gpt-6-luna"]'),($2,$3,19,'["gpt-6-astra"]'),($1,$4,3,NULL)`, a.ID, b.ID, source.ID, target.ID)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, "UPDATE accounts SET status='error',schedulable=false WHERE id=$1", b.ID)
	require.NoError(t, err)
	sourceBefore, targetBefore := poolGroupRow(t, ctx, repo, source.ID), poolGroupRow(t, ctx, repo, target.ID)
	sourceBindings := poolBindingsRow(t, ctx, repo, source.ID)
	accounts := newAccountRepositoryWithSQL(client, tx, nil)
	aBefore, bBefore := nativeRateLimitAccountRow(t, ctx, accounts, a.ID), nativeRateLimitAccountRow(t, ctx, accounts, b.ID)
	o := service.ShareAccountPoolInput{SourceGroupID: source.ID, ExpectedSourceAccountIDs: []int64{b.ID, a.ID}, ExpectedTargetAccountIDs: []int64{a.ID}}
	result, err := repo.ShareAccountPoolIfObserved(dbent.NewTxContext(ctx, tx), target.ID, o)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, []int64{b.ID}, result.AddedAccountIDs)
	var priority int
	var allowed string
	require.NoError(t, scanSingleRow(ctx, client, "SELECT priority,COALESCE(allowed_models,'null'::jsonb)::text FROM account_groups WHERE group_id=$1 AND account_id=$2", []any{target.ID, a.ID}, &priority, &allowed))
	require.Equal(t, 3, priority)
	require.Equal(t, "null", allowed)
	require.NoError(t, scanSingleRow(ctx, client, "SELECT priority,allowed_models::text FROM account_groups WHERE group_id=$1 AND account_id=$2", []any{target.ID, b.ID}, &priority, &allowed))
	require.Equal(t, 19, priority)
	require.Equal(t, `["gpt-6-astra"]`, allowed)
	require.Equal(t, sourceBefore, poolGroupRow(t, ctx, repo, source.ID))
	require.Equal(t, targetBefore, poolGroupRow(t, ctx, repo, target.ID))
	require.Equal(t, sourceBindings, poolBindingsRow(t, ctx, repo, source.ID))
	require.Equal(t, aBefore, nativeRateLimitAccountRow(t, ctx, accounts, a.ID))
	require.Equal(t, bBefore, nativeRateLimitAccountRow(t, ctx, accounts, b.ID))
	stale, err := repo.ShareAccountPoolIfObserved(dbent.NewTxContext(ctx, tx), target.ID, o)
	require.NoError(t, err)
	require.False(t, stale.Applied)
	require.Equal(t, "account_pool_changed", stale.Reason)
	o.ExpectedTargetAccountIDs = []int64{a.ID, b.ID}
	again, err := repo.ShareAccountPoolIfObserved(dbent.NewTxContext(ctx, tx), target.ID, o)
	require.NoError(t, err)
	require.Equal(t, "already_shared", again.Reason)
}
func TestShareAccountPoolRejectsDeletedAccount(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newGroupRepositoryWithSQL(client, tx)
	source := mustCreateGroup(t, client, &service.Group{Name: "share-reject-src", Platform: service.PlatformOpenAI, Status: service.StatusActive})
	target := mustCreateGroup(t, client, &service.Group{Name: "share-reject-dst", Platform: service.PlatformOpenAI, Status: service.StatusActive})
	a := mustCreateAccount(t, client, &service.Account{Name: "badpool", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	_, err := client.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,11)", a.ID, source.ID)
	require.NoError(t, err)
	_, err = client.ExecContext(ctx, "UPDATE accounts SET deleted_at=NOW() WHERE id=$1", a.ID)
	require.NoError(t, err)
	result, err := repo.ShareAccountPoolIfObserved(dbent.NewTxContext(ctx, tx), target.ID, service.ShareAccountPoolInput{SourceGroupID: source.ID, ExpectedSourceAccountIDs: []int64{a.ID}, ExpectedTargetAccountIDs: []int64{}})
	require.NoError(t, err)
	require.Equal(t, "account_unavailable", result.Reason)
	require.Equal(t, "[]", poolBindingsRow(t, ctx, repo, target.ID))
}
func TestShareAccountPoolConcurrentBindingChangeLosesCAS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := testEntClient(t)
	repo := newGroupRepositoryWithSQL(client, integrationDB)
	source := mustCreateGroup(t, client, &service.Group{Name: "share-concurrent-src", Platform: service.PlatformOpenAI, Status: service.StatusActive})
	target := mustCreateGroup(t, client, &service.Group{Name: "share-concurrent-dst", Platform: service.PlatformOpenAI, Status: service.StatusActive})
	a := mustCreateAccount(t, client, &service.Account{Name: "concurrentpool", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, e := integrationDB.ExecContext(cleanup, "DELETE FROM scheduler_outbox WHERE group_id IN ($1,$2) OR account_id=$3", source.ID, target.ID, a.ID)
		require.NoError(t, e)
		_, e = integrationDB.ExecContext(cleanup, "DELETE FROM account_groups WHERE group_id IN ($1,$2)", source.ID, target.ID)
		require.NoError(t, e)
		_, e = integrationDB.ExecContext(cleanup, "DELETE FROM accounts WHERE id=$1", a.ID)
		require.NoError(t, e)
		_, e = integrationDB.ExecContext(cleanup, "DELETE FROM groups WHERE id IN ($1,$2)", source.ID, target.ID)
		require.NoError(t, e)
	})
	_, err := integrationDB.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,11)", a.ID, source.ID)
	require.NoError(t, err)
	writer, err := client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = writer.Rollback() }()
	_, err = writer.ExecContext(ctx, "SELECT id FROM groups WHERE id=$1 FOR UPDATE", target.ID)
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, "INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,33)", a.ID, target.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		r, e := repo.ShareAccountPoolIfObserved(ctx, target.ID, service.ShareAccountPoolInput{SourceGroupID: source.ID, ExpectedSourceAccountIDs: []int64{a.ID}, ExpectedTargetAccountIDs: []int64{}})
		if e == nil && (r.Applied || r.Reason != "account_pool_changed") {
			e = fmt.Errorf("unexpected CAS result: %+v", r)
		}
		done <- e
	}()
	require.Eventually(t, func() bool {
		var count int
		e := integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%id,platform,status FROM groups%'").Scan(&count)
		require.NoError(t, e)
		return count > 0
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit())
	require.NoError(t, <-done)
	var priority int
	require.NoError(t, scanSingleRow(ctx, repo.sql, "SELECT priority FROM account_groups WHERE group_id=$1 AND account_id=$2", []any{target.ID, a.ID}, &priority))
	require.Equal(t, 33, priority)
}
