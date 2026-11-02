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

func TestDashboardTodayStats_BoundedDSTDaysPreserveLifetime(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		month  time.Month
		day    int
		hours  time.Duration
		amount float64
	}{
		{"spring_zero", time.March, 8, 23, 0},
		{"fall_large", time.November, 1, 25, 1234567.8912},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := newUsageLogRepositoryWithSQL(nil, db)
			start := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, loc)
			end := start.AddDate(0, 0, 1)
			require.Equal(t, tc.hours*time.Hour, end.Sub(start))
			mock.ExpectQuery(`(?s)SELECT.*COALESCE\(SUM\(actual_cost\), 0\).*COALESCE\(account_stats_cost, total_cost\).*COUNT\(DISTINCT user_id\).*FROM usage_logs\s+WHERE created_at >= \$1 AND created_at < \$2`).
				WithArgs(start.UTC(), end.UTC()).
				WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual", "account", "users"}).AddRow(7, 10, 20, 30, 40, 99, tc.amount, 55, 3))
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE deleted_at IS NULL AND created_at >= \$1 AND created_at < \$2`).
				WithArgs(start.UTC(), end.UTC()).WillReturnRows(sqlmock.NewRows([]string{"new_users"}).AddRow(2))
			stats := &DashboardStats{TotalActualCost: 9876543.21, TotalRequests: 900, TodayActualCost: 42.71651945, HourlyActiveUsers: 4}
			require.NoError(t, repo.fillDashboardTodayStats(context.Background(), stats, start.UTC(), end.UTC()))
			require.Equal(t, tc.amount, stats.TodayActualCost)
			require.Equal(t, int64(7), stats.TodayRequests)
			require.Equal(t, int64(100), stats.TodayTokens)
			require.Equal(t, 55.0, stats.TodayAccountCost)
			require.Equal(t, int64(3), stats.ActiveUsers)
			require.Equal(t, int64(2), stats.TodayNewUsers)
			require.Equal(t, 9876543.21, stats.TotalActualCost)
			require.Equal(t, int64(900), stats.TotalRequests)
			require.Equal(t, int64(4), stats.HourlyActiveUsers)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUserDashboardStats_RequestDayBoundsIncludePlatform(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	ctx := timezone.WithUserDay(context.Background(), "Europe/Stockholm")
	start, end := timezone.DayRange(ctx)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_keys WHERE user_id = \$1 AND deleted_at IS NULL`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_keys WHERE user_id = \$1 AND status = \$2 AND deleted_at IS NULL`).WithArgs(int64(7), "active").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE user_id = \$1\s*$`).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual", "duration"}).AddRow(900, 10, 20, 30, 40, 2000000, 1234567.8912, 12))
	mock.ExpectQuery(`(?s)SELECT.*SUM\(actual_cost\).*FROM usage_logs\s+WHERE user_id = \$1 AND created_at >= \$2 AND created_at < \$3`).WithArgs(int64(7), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual"}).AddRow(7, 1, 2, 3, 4, 2000, 1234.5678))
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE created_at >= \$1 AND user_id = \$2`).WithArgs(sqlmock.AnyArg(), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "tokens"}).AddRow(5, 10))
	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\) FILTER \(WHERE ul.created_at >= \$2 AND ul.created_at < \$3\).*SUM\(ul.actual_cost\) FILTER \(WHERE ul.created_at >= \$2 AND ul.created_at < \$3\).*FROM usage_logs ul`).WithArgs(int64(7), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "requests", "tokens", "actual", "today_requests", "today_tokens", "today_actual"}).AddRow("openai", 900, 100, 1234567.8912, 7, 10, 1234.5678))
	stats, err := repo.GetUserDashboardStats(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 1234567.8912, stats.TotalActualCost)
	require.Equal(t, 1234.5678, stats.TodayActualCost)
	require.Equal(t, int64(10), stats.TodayTokens)
	require.Len(t, stats.ByPlatform, 1)
	require.Equal(t, stats.TodayActualCost, stats.ByPlatform[0].TodayActualCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardStats_RollupTotalsWithRequestDayOverride(t *testing.T) {
	for _, sameDay := range []bool{true, false} {
		name := "different_day"
		if sameDay {
			name = "same_day"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := newUsageLogRepositoryWithSQL(nil, db)
			ctx := timezone.WithUserDay(context.Background(), "")
			serverStart := timezone.Today()
			if !sameDay {
				ctx = timezone.WithUserDay(context.Background(), "UTC")
				day, _ := timezone.DayRange(ctx)
				if day.Equal(serverStart) {
					ctx = timezone.WithUserDay(context.Background(), "Pacific/Honolulu")
				}
			}
			start, end := timezone.DayRange(ctx)
			mock.ExpectQuery(`(?s)SELECT.*FROM users\s+WHERE deleted_at IS NULL`).WithArgs(serverStart, serverStart.AddDate(0, 0, 1)).WillReturnRows(sqlmock.NewRows([]string{"total", "today"}).AddRow(90, 9))
			mock.ExpectQuery(`(?s)SELECT.*FROM api_keys\s+WHERE deleted_at IS NULL`).WithArgs("active").WillReturnRows(sqlmock.NewRows([]string{"total", "active"}).AddRow(90, 80))
			mock.ExpectQuery(`(?s)SELECT.*FROM accounts\s+WHERE deleted_at IS NULL`).WithArgs("active", "error", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"total", "normal", "error", "limit", "overload"}).AddRow(90, 80, 1, 2, 3))
			mock.ExpectQuery(`(?s)SELECT.*SUM\(total_requests\).*FROM usage_dashboard_daily`).WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual", "account", "duration"}).AddRow(900, 10, 20, 30, 40, 2000000, 1234567.8912, 1000, 9000))
			mock.ExpectQuery(`(?s)SELECT.*FROM usage_dashboard_daily\s+WHERE bucket_date = \$1::date`).WithArgs(serverStart).WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual", "account", "users"}).AddRow(797, 1, 2, 3, 4, 99, 42.71651945, 55, 3))
			mock.ExpectQuery(`SELECT active_users FROM usage_dashboard_hourly WHERE bucket_start = \$1`).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(4))
			if !sameDay {
				mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE created_at >= \$1 AND created_at < \$2`).WithArgs(start.UTC(), end.UTC()).WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual", "account", "users"}).AddRow(817, 10, 20, 30, 40, 999, 928.993, 555, 30))
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE deleted_at IS NULL AND created_at >= \$1 AND created_at < \$2`).WithArgs(start.UTC(), end.UTC()).WillReturnRows(sqlmock.NewRows([]string{"users"}).AddRow(20))
			}
			mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE created_at >= \$1\s*$`).WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"requests", "tokens"}).AddRow(5, 10))
			stats, err := repo.GetDashboardStats(ctx)
			require.NoError(t, err)
			require.Equal(t, 1234567.8912, stats.TotalActualCost)
			require.Equal(t, int64(900), stats.TotalRequests)
			require.Equal(t, int64(4), stats.HourlyActiveUsers)
			if sameDay {
				require.Equal(t, 42.71651945, stats.TodayActualCost)
			} else {
				require.Equal(t, 928.993, stats.TodayActualCost)
				require.Equal(t, int64(817), stats.TodayRequests)
				require.Equal(t, int64(20), stats.TodayNewUsers)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAPIKeyDashboardStats_RequestDayBoundsPreserveLifetime(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	ctx := timezone.WithUserDay(context.Background(), "Europe/Stockholm")
	start, end := timezone.DayRange(ctx)
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE api_key_id = \$1\s*$`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual", "duration"}).AddRow(900, 10, 20, 30, 40, 2000000, 1234567.8912, 12))
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE api_key_id = \$1 AND created_at >= \$2 AND created_at < \$3`).WithArgs(int64(7), start, end).WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "creation", "read", "cost", "actual"}).AddRow(0, 0, 0, 0, 0, 0, 0))
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs\s+WHERE created_at >= \$1 AND api_key_id = \$2`).WithArgs(sqlmock.AnyArg(), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"requests", "tokens"}).AddRow(0, 0))
	stats, err := repo.GetAPIKeyDashboardStats(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 1234567.8912, stats.TotalActualCost)
	require.Zero(t, stats.TodayActualCost)
	require.Zero(t, stats.TodayRequests)
	require.NoError(t, mock.ExpectationsWereMet())
}
