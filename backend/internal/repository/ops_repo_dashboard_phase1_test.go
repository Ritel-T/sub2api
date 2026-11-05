package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOpsLatencyReportsMeasuredSampleCounts(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	mock.ExpectQuery(`COUNT\(duration_ms\) AS duration_sample_count`).WillReturnRows(sqlmock.NewRows([]string{"d50", "d90", "d95", "d99", "dav", "dmax", "t50", "t90", "t95", "t99", "tav", "tmax", "tn", "dn"}).AddRow(100, 200, 300, 400, 250, 500, 10, 20, 30, 40, 25, 50, 3, 5))
	now := time.Now()
	duration, ttft, n, err := repo.queryUsageLatency(context.Background(), &service.OpsDashboardFilter{}, now.Add(-time.Hour), now)
	require.NoError(t, err)
	require.EqualValues(t, 5, *duration.SampleCount)
	require.EqualValues(t, 3, *ttft.SampleCount)
	require.EqualValues(t, 3, n)
	require.Equal(t, "raw_exact", duration.PercentileMethod)
	require.Equal(t, "raw_exact", ttft.PercentileMethod)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsOutputTPSSamplingContract(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	mock.ExpectQuery(`(?s)output_tokens >= 2.*billing_mode.*NOT IN \('image', 'video'\).*request_type IN \(1, 2, 3\)`).WillReturnRows(sqlmock.NewRows([]string{"p5", "p10", "p50", "avg", "n"}).AddRow(1, 2, 3, 4, 5))
	now := time.Now()
	_, err := repo.queryOutputTPS(context.Background(), &service.OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
