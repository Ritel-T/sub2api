//go:build unit

package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPanelTrendsUseRequestTimezoneWithPreciseUTCRange(t *testing.T) {
	for _, zone := range []string{"America/New_York", "Asia/Kathmandu"} {
		t.Run(zone, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := newUsageLogRepositoryWithSQL(nil, db)
			ctx := timezone.WithUserDay(context.Background(), zone)
			// Explicit UTC bounds must not erase the selected display calendar.
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			mock.ExpectQuery(`(?s)TO_CHAR\(created_at AT TIME ZONE \$3.*FROM usage_logs`).WithArgs(start, end, zone).
				WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input", "output", "cache_creation", "cache_read", "tokens", "cost", "actual"}).AddRow("2026-10-31", 1, 1, 2, 0, 0, 3, 0.1, 0.1))
			rows, err := repo.GetUsageTrendWithFilters(ctx, start, end, "day", 0, 0, 0, 0, "", nil, nil, nil)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, "2026-10-31", rows[0].Date)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPanelBatchKeyTodayUsesUserDay(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	ctx := timezone.WithUserDay(context.Background(), "Asia/Kathmandu")
	start, _ := timezone.DayRange(ctx)
	mock.ExpectQuery(`(?s)FROM usage_logs.*WHERE api_key_id = ANY`).WithArgs("{7}", time.Time{}, sqlmock.AnyArg(), start).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "total_cost", "today_cost"}).AddRow(7, 10, 2))
	rows, err := newUsageLogRepositoryWithSQL(nil, db).GetBatchAPIKeyUsageStats(ctx, []int64{7}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Equal(t, 2.0, rows[7].TodayActualCost)
	require.NoError(t, mock.ExpectationsWereMet())
}
