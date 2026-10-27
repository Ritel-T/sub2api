//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestBatchAPIKeyStats_DefaultIncludesAllRetainedHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	mock.ExpectQuery(`(?s)SELECT.*api_key_id.*FROM usage_logs.*WHERE api_key_id = ANY`).
		WithArgs("{7,8}", time.Time{}, sqlmock.AnyArg(), timezone.Today()).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "total_cost", "today_cost"}).AddRow(7, 15.25, 2.5))
	stats, err := repo.GetBatchAPIKeyUsageStats(context.Background(), []int64{7, 8, 7, 0, -1}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, stats, 2)
	require.Equal(t, 15.25, stats[7].TotalActualCost)
	require.Equal(t, 2.5, stats[7].TodayActualCost)
	require.Zero(t, stats[8].TotalActualCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchAPIKeyStats_PreservesExplicitRangeAndQueryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start := time.Now().Add(-time.Hour)
	end := time.Now()
	mock.ExpectQuery(`(?s)SELECT.*created_at >= \$2 AND created_at < \$3.*FROM usage_logs`).
		WithArgs("{7}", start, end, timezone.Today()).
		WillReturnError(context.DeadlineExceeded)
	stats, err := repo.GetBatchAPIKeyUsageStats(context.Background(), []int64{7}, start, end)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}
