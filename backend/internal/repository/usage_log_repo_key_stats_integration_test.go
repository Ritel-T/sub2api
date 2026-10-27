//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchAPIKeyStats_RecordedTotalIncludesOlderThanThirtyDaysAndHonorsRange(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "recorded-key-stats@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-recorded-key-stats", Name: "history"})
	account := mustCreateAccount(t, client, &service.Account{Name: "recorded-key-stats"})
	now := time.Now()
	for i, sample := range []struct {
		at   time.Time
		cost float64
	}{
		{now.AddDate(0, 0, -45), 7.5}, {timezone.Today().Add(-time.Hour), 2.25}, {now.Add(-time.Second), 1.125},
	} {
		_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
			RequestID: []string{"recorded-stats-old", "recorded-stats-yesterday", "recorded-stats-today"}[i],
			Model:     "stats-model", ActualCost: sample.cost, TotalCost: sample.cost, CreatedAt: sample.at})
		require.NoError(t, err)
	}
	stats, err := repo.GetBatchAPIKeyUsageStats(ctx, []int64{key.ID}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 10.875, stats[key.ID].TotalActualCost, 0.0000001)
	require.InDelta(t, 1.125, stats[key.ID].TodayActualCost, 0.0000001)
	recent, err := repo.GetBatchAPIKeyUsageStats(ctx, []int64{key.ID}, now.AddDate(0, 0, -30), now)
	require.NoError(t, err)
	require.InDelta(t, 3.375, recent[key.ID].TotalActualCost, 0.0000001)
	require.InDelta(t, 1.125, recent[key.ID].TodayActualCost, 0.0000001)
}
