//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountAPIEquivalent_ReadSideHistoricalAndNewRows(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "api-equivalence@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-api-equivalence", Name: "test"})
	account := mustCreateAccount(t, client, &service.Account{Name: "equivalence", Platform: service.PlatformOpenAI})
	empty := mustCreateAccount(t, client, &service.Account{Name: "empty-equivalence"})
	str := func(v string) *string { return &v }
	old := time.Now().UTC().Truncate(24 * time.Hour).Add(-48 * time.Hour)
	peakDate := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	logs := []struct {
		name     string
		log      service.UsageLog
		want     float64
		unpriced bool
	}{
		{name: "threshold strictly exclusive", log: service.UsageLog{Model: "gpt-6-astra", InputTokens: 272000, OutputTokens: 100}, want: 2.725},
		{name: "cache contributes to context", log: service.UsageLog{Model: "gpt-6-astra", InputTokens: 20000, CacheReadTokens: 252001, OutputTokens: 100}, want: .911502},
		{name: "cache creation contributes to context", log: service.UsageLog{Model: "gpt-6-astra", InputTokens: 272000, CacheCreationTokens: 1, OutputTokens: 100}, want: 5.447525},
		{name: "observed model overrides mapped and requested", log: service.UsageLog{Model: "gpt-6-astra", UpstreamModel: str("gpt-6-sol"), UpstreamResponseModel: str("gpt-6.1-sol"), InputTokens: 1000, CacheReadTokens: 1000, OutputTokens: 100}, want: .0031},
		{name: "upstream model overrides client alias", log: service.UsageLog{Model: "gpt-6-sol", UpstreamModel: str("gpt-6.1-sol"), InputTokens: 1000, CacheReadTokens: 1000, OutputTokens: 100}, want: .0031},
		{name: "priority is official multiplier", log: service.UsageLog{Model: "gpt-6-astra", ServiceTier: str("priority"), InputTokens: 1000, OutputTokens: 100}, want: .03},
		{name: "astra ultrafast", log: service.UsageLog{Model: "gpt-6-astra", ServiceTier: str("ultrafast"), InputTokens: 1000, OutputTokens: 100}, want: .09},
		{name: "openai one-hour cache is same write price", log: service.UsageLog{Model: "gpt-6-astra", CacheCreationTokens: 1000, CacheCreation1hTokens: 1000}, want: .0125},
		{name: "dated effort alias", log: service.UsageLog{Model: "gpt-6-astra-high-2026-10-01", InputTokens: 1000, OutputTokens: 100}, want: .015},
		{name: "flex official discount", log: service.UsageLog{Model: "gpt-6-astra", InputTokens: 1000, OutputTokens: 100, ServiceTier: str("flex")}, want: .0075},
		{name: "sol56 uses official not retail fallback", log: service.UsageLog{Model: "gpt-5.6-sol", InputTokens: 1000, OutputTokens: 100}, want: .006},
		{name: "claude one-hour cache", log: service.UsageLog{Model: "claude-opus-4-6", InputTokens: 1000, CacheCreationTokens: 2000, CacheCreation5mTokens: 1000, CacheCreation1hTokens: 1000, CacheReadTokens: 1000, OutputTokens: 100}, want: .02425},
		{name: "grok inclusive threshold", log: service.UsageLog{Model: "grok-4.6", InputTokens: 199999, CacheReadTokens: 1, OutputTokens: 1}, want: .800009},
		{name: "deepseek peak at start", log: service.UsageLog{Model: "deepseek-flash", InputTokens: 1000, CacheReadTokens: 1000, OutputTokens: 100, CreatedAt: peakDate.Add(time.Hour)}, want: .000426},
		{name: "deepseek offpeak at end", log: service.UsageLog{Model: "deepseek-flash", InputTokens: 1000, CacheReadTokens: 1000, OutputTokens: 100, CreatedAt: peakDate.Add(4 * time.Hour)}, want: .000213},
		{name: "unknown tokenless station fee", log: service.UsageLog{Model: "station-per-request"}, want: 0},
		{name: "strict unknown codex model", log: service.UsageLog{Model: "codex-auto-review", InputTokens: 1000}, unpriced: true},
		{name: "unknown observed model cannot use known requested", log: service.UsageLog{Model: "gpt-6-astra", UpstreamResponseModel: str("codex-auto-review"), InputTokens: 1000}, unpriced: true},
		{name: "gpt55 priority not publicly verified", log: service.UsageLog{Model: "gpt-5.5", InputTokens: 1000, ServiceTier: str("priority")}, unpriced: true},
		{name: "media billing without count is unpriced", log: service.UsageLog{Model: "gpt-6-astra", BillingMode: str("image")}, unpriced: true},
		{name: "invalid cache breakdown", log: service.UsageLog{Model: "gpt-6-astra", CacheCreationTokens: 1000, CacheCreation1hTokens: 2000}, unpriced: true},
		{name: "unknown tier", log: service.UsageLog{Model: "gpt-6-astra", ServiceTier: str("scale"), InputTokens: 1000}, unpriced: true},
		{name: "unknown image pricing", log: service.UsageLog{Model: "gpt-image-2.5", ImageCount: 1}, unpriced: true},
		{name: "unknown write price is not free", log: service.UsageLog{Model: "deepseek-flash", CacheCreationTokens: 1000}, unpriced: true},
	}
	var knownSum float64
	var missing int64
	rate := 7.0
	statsCost := 123.0
	for i, tc := range logs {
		tc.log.UserID, tc.log.APIKeyID, tc.log.AccountID = user.ID, key.ID, account.ID
		tc.log.RequestID = fmt.Sprintf("api-equivalent-%d", i)
		if tc.log.CreatedAt.IsZero() {
			tc.log.CreatedAt = old.Add(time.Duration(i) * time.Second)
		}
		tc.log.TotalCost, tc.log.ActualCost = 99, 11
		tc.log.AccountStatsCost, tc.log.AccountRateMultiplier = &statsCost, &rate
		_, err := repo.Create(ctx, &tc.log)
		require.NoError(t, err, tc.name)
		var got *float64
		require.NoError(t, scanSingleRow(ctx, repo.sql, accountAPIEquivalentRowsCTE("ul.request_id = $1 AND ul.api_key_id = $2")+"SELECT api_equivalent_row_cost FROM api_equivalent_rows", []any{tc.log.RequestID, key.ID}, &got), tc.name)
		if tc.unpriced {
			require.Nil(t, got, tc.name)
			missing++
		} else {
			require.NotNil(t, got, tc.name)
			require.InDelta(t, tc.want, *got, 1e-10, tc.name)
			knownSum += tc.want
		}
	}
	// No persisted field, including customer and account pricing, is changed.
	var beforeRows string
	const rowsDigest = "SELECT MD5(STRING_AGG(TO_JSONB(ul)::text, ',' ORDER BY id)) FROM usage_logs ul WHERE account_id = $1"
	require.NoError(t, scanSingleRow(ctx, repo.sql, rowsDigest, []any{account.ID}, &beforeRows))
	stats, err := repo.GetAccountWindowStats(ctx, account.ID, time.Time{})
	require.NoError(t, err)
	require.NotNil(t, stats.APIEquivalentCost)
	require.InDelta(t, knownSum, *stats.APIEquivalentCost, 1e-10)
	require.Equal(t, missing, stats.APIEquivalentUnpricedRequests)
	require.InDelta(t, float64(len(logs))*123*7, stats.Cost, 1e-9)
	require.InDelta(t, float64(len(logs))*99, stats.StandardCost, 1e-9)
	require.InDelta(t, float64(len(logs))*11, stats.UserCost, 1e-9)
	batch, err := repo.GetAccountWindowStatsBatch(ctx, []int64{account.ID, empty.ID}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, stats, batch[account.ID])
	require.NotNil(t, batch[empty.ID].APIEquivalentCost)
	require.Zero(t, *batch[empty.ID].APIEquivalentCost)
	require.Zero(t, batch[empty.ID].APIEquivalentUnpricedRequests)
	var total, actual, accountCost, accountRate float64
	require.NoError(t, scanSingleRow(ctx, repo.sql, "SELECT total_cost, actual_cost, account_stats_cost, account_rate_multiplier FROM usage_logs WHERE account_id=$1 LIMIT 1", []any{account.ID}, &total, &actual, &accountCost, &accountRate))
	require.Equal(t, []float64{99, 11, 123, 7}, []float64{total, actual, accountCost, accountRate})

	var afterRows string
	require.NoError(t, scanSingleRow(ctx, repo.sql, rowsDigest, []any{account.ID}, &afterRows))
	require.Equal(t, beforeRows, afterRows)
	emptyStats, err := repo.GetAccountWindowStats(ctx, empty.ID, time.Time{})
	require.NoError(t, err)
	require.NotNil(t, emptyStats.APIEquivalentCost)
	require.Zero(t, *emptyStats.APIEquivalentCost)

	// A new row is included without a backfill or any special writer.
	todayLog := service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: "new-row", Model: "gpt-6-astra", InputTokens: 1000, OutputTokens: 100, CreatedAt: time.Now().UTC(), TotalCost: 99, ActualCost: 11}
	_, err = repo.Create(ctx, &todayLog)
	require.NoError(t, err)
	todayStats, err := repo.GetAccountTodayStats(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, todayStats.APIEquivalentCost)
	require.InDelta(t, .015, *todayStats.APIEquivalentCost, 1e-10)
	require.Zero(t, todayStats.APIEquivalentUnpricedRequests)
}

func TestAccountAPIEquivalent_AllUnpricedIsNull(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "api-unpriced@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-api-unpriced", Name: "test"})
	account := mustCreateAccount(t, client, &service.Account{Name: "unpriced"})
	_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Model: "codex-auto-review", InputTokens: 100, CreatedAt: time.Now().UTC()})
	require.NoError(t, err)
	stats, err := repo.GetAccountWindowStats(ctx, account.ID, time.Time{})
	require.NoError(t, err)
	require.Nil(t, stats.APIEquivalentCost)
	require.Equal(t, int64(1), stats.APIEquivalentUnpricedRequests)
}
