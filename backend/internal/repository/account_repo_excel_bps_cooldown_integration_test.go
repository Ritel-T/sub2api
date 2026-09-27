//go:build integration

package repository

import (
	"context"
	"maps"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newExcelBPSCooldownIntegrationAccount(t *testing.T) (*accountRepository, *service.Account) {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{
		Name: t.Name(), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}},
		Extra:       map[string]any{"openai_excel_bps": true, "model_rate_limits": map[string]any{"test-model": "keep"}},
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
		require.NoError(t, err)
		require.NoError(t, client.Account.DeleteOneID(account.ID).Exec(ctx))
	})
	_, err := integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
	require.NoError(t, err)
	return newAccountRepositoryWithSQL(client, integrationDB, nil), account
}

func excelBPSCooldownEventCount(t *testing.T, ctx context.Context, repo *accountRepository, accountID int64) int {
	t.Helper()
	var count int
	require.NoError(t, scanSingleRow(ctx, clientFromContext(ctx, repo.client),
		"SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1 AND event_type = $2",
		[]any{accountID, service.SchedulerOutboxEventAccountChanged}, &count))
	return count
}

func TestExcelBPSCooldownRepositoryPreservesIndependentStateAndRefreshesCache(t *testing.T) {
	ctx := context.Background()
	repo, input := newExcelBPSCooldownIntegrationAccount(t)
	nativeUntil := time.Now().UTC().Truncate(time.Second).Add(24 * time.Hour)
	_, err := repo.client.Account.UpdateOneID(input.ID).
		SetRateLimitedAt(nativeUntil.Add(-time.Hour)).SetRateLimitResetAt(nativeUntil).
		SetTempUnschedulableUntil(nativeUntil).SetTempUnschedulableReason("unrelated").
		SetStatus(service.StatusError).SetSchedulable(false).Save(ctx)
	require.NoError(t, err)
	before, err := repo.GetByID(ctx, input.ID)
	require.NoError(t, err)
	cache := &schedulerCacheRecorder{}
	repo.schedulerCache = cache
	until := nativeUntil.Add(time.Nanosecond)
	stored, err := repo.ExtendExcelBPSRateLimit(ctx, input.ID, until, "quota_exhausted")
	require.NoError(t, err)
	require.True(t, stored.Equal(until))
	after, err := repo.GetByID(ctx, input.ID)
	require.NoError(t, err)
	wantExtra := maps.Clone(before.Extra)
	wantExtra[excelBPSCooldownResetKey] = until.Format(time.RFC3339Nano)
	wantExtra[excelBPSCooldownReasonKey] = "quota_exhausted"
	require.Equal(t, wantExtra, after.Extra)
	require.Equal(t, before.Credentials, after.Credentials)
	require.Equal(t, before.GroupIDs, after.GroupIDs)
	require.Equal(t, before.ProxyID, after.ProxyID)
	require.Equal(t, before.RateLimitResetAt, after.RateLimitResetAt)
	require.Equal(t, before.RateLimitedAt, after.RateLimitedAt)
	require.Equal(t, before.TempUnschedulableUntil, after.TempUnschedulableUntil)
	require.Equal(t, before.TempUnschedulableReason, after.TempUnschedulableReason)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.Schedulable, after.Schedulable)
	require.Equal(t, after.Extra, cache.accounts[input.ID].Extra)
	require.Equal(t, 1, excelBPSCooldownEventCount(t, ctx, repo, input.ID))
	// The shorter observation must preserve the reason, and refresh its snapshot
	// from the database rather than an account captured before the request.
	_, err = repo.client.Account.UpdateOneID(input.ID).SetName("changed-after-observation").Save(ctx)
	require.NoError(t, err)
	stored, err = repo.ExtendExcelBPSRateLimit(ctx, input.ID, until.Add(-time.Hour), "rate_limited")
	require.NoError(t, err)
	require.True(t, stored.Equal(until))
	require.Equal(t, "changed-after-observation", cache.accounts[input.ID].Name)
	require.Equal(t, wantExtra, cache.accounts[input.ID].Extra)
	require.Equal(t, 1, excelBPSCooldownEventCount(t, ctx, repo, input.ID))
	require.NotContains(t, input.Extra, excelBPSCooldownResetKey)
}

func TestExcelBPSCooldownRepositoryConcurrentExtensionsAndExtraUpdates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, account := newExcelBPSCooldownIntegrationAccount(t)
	const requests = 24
	base := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	maximum := base.Add(requests * time.Nanosecond)
	type outcome struct {
		proposed, stored time.Time
		err              error
	}
	results := make(chan outcome, requests+1)
	start := make(chan struct{})
	for i := 1; i <= requests; i++ {
		until := base.Add(time.Duration(i) * time.Nanosecond)
		reason := "rate_limited"
		if i == requests {
			reason = "quota_exhausted"
		}
		go func() {
			<-start
			stored, err := repo.ExtendExcelBPSRateLimit(ctx, account.ID, until, reason)
			results <- outcome{until, stored, err}
		}()
	}
	go func() {
		<-start
		err := repo.UpdateExtra(ctx, account.ID, map[string]any{"cooldown_test_unrelated": true})
		results <- outcome{err: err}
	}()
	close(start)
	// Drain every worker before cleanup, even if one failed.
	for range requests + 1 {
		result := <-results
		if result.err != nil {
			t.Errorf("concurrent update failed: %v", result.err)
			continue
		}
		if !result.proposed.IsZero() && (result.stored.Before(result.proposed) || result.stored.After(maximum)) {
			t.Errorf("stored deadline %s outside [%s, %s]", result.stored, result.proposed, maximum)
		}
	}
	if t.Failed() {
		return
	}
	after, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, maximum.Format(time.RFC3339Nano), after.Extra[excelBPSCooldownResetKey])
	require.Equal(t, "quota_exhausted", after.Extra[excelBPSCooldownReasonKey])
	require.Equal(t, true, after.Extra["cooldown_test_unrelated"])
	require.Equal(t, account.Extra["model_rate_limits"], after.Extra["model_rate_limits"])
	// Reuse the existing account_changed deduplication contract.
	require.Equal(t, 1, excelBPSCooldownEventCount(t, ctx, repo, account.ID))
	stored, err := repo.ExtendExcelBPSRateLimit(ctx, account.ID, base, "rate_limited")
	require.NoError(t, err)
	require.True(t, stored.Equal(maximum))
}

func TestExcelBPSCooldownRepositoryOuterTransactionIsAtomic(t *testing.T) {
	for _, mode := range []string{"commit", "rollback", "transaction client"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo, account := newExcelBPSCooldownIntegrationAccount(t)
			cache := &schedulerCacheRecorder{}
			repo.schedulerCache = cache
			tx, err := repo.client.Tx(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			txCtx := dbent.NewTxContext(ctx, tx)
			writer, writeCtx := repo, txCtx
			if mode == "transaction client" {
				writer = newAccountRepositoryWithSQL(tx.Client(), tx, cache)
				writeCtx = ctx
			}
			until := time.Now().UTC().Add(time.Hour)
			stored, err := writer.ExtendExcelBPSRateLimit(writeCtx, account.ID, until, "rate_limited")
			require.NoError(t, err)
			require.True(t, stored.Equal(until))
			require.Equal(t, 1, excelBPSCooldownEventCount(t, txCtx, repo, account.ID))
			uncommitted, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			require.NotContains(t, uncommitted.Extra, excelBPSCooldownResetKey)
			require.Zero(t, excelBPSCooldownEventCount(t, ctx, repo, account.ID))
			require.Empty(t, cache.setAccounts, "never publish uncommitted state")
			if mode == "commit" {
				require.NoError(t, tx.Commit())
			} else {
				require.NoError(t, tx.Rollback())
			}
			after, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			if mode == "commit" {
				require.Equal(t, until.Format(time.RFC3339Nano), after.Extra[excelBPSCooldownResetKey])
				require.Equal(t, 1, excelBPSCooldownEventCount(t, ctx, repo, account.ID))
			} else {
				require.NotContains(t, after.Extra, excelBPSCooldownResetKey)
				require.Zero(t, excelBPSCooldownEventCount(t, ctx, repo, account.ID))
			}
			require.Empty(t, cache.setAccounts, "caller-owned commits propagate through the outbox")
		})
	}
}
