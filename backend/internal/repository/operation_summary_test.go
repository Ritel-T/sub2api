package repository

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestOperationSummaryUsesOnlyPhysicalVersionedSamples(t *testing.T) {
	now := time.Now()
	f := service.ConnectionTraceFilter{Start: now.Add(-time.Hour), End: now, UserID: 7}
	q, args := operationSummaryQuery(f)
	require.NotContains(t, q, "UNION")
	require.NotContains(t, q, "usage_logs")
	require.Contains(t, q, "source='application'")
	require.Contains(t, q, "operation'->>'version'='2'")
	require.Len(t, args, 3)
	require.True(t, strings.Index(connectionStateSQL, "operation") < strings.Index(connectionStateSQL, "downstream_error"))
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	mock.ExpectQuery("SELECT COALESCE").WithArgs(f.Start, f.End, int64(7)).WillReturnRows(sqlmock.NewRows([]string{"outcome", "count", "recovered"}).AddRow("completed", 2, 1).AddRow("unknown", 1, 0))
	got := repo.ObservedOperationSummary(context.Background(), f)
	require.Equal(t, "observed", got.Status)
	require.Equal(t, "unknown", got.Coverage)
	require.EqualValues(t, 3, got.SampleCount)
	require.EqualValues(t, 1, got.RecoveredCount)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestOperationSummaryRejectsLongWindowWithoutQuery(t *testing.T) {
	repo := newUsageLogRepositoryWithSQL(nil, nil)
	now := time.Now()
	got := repo.ObservedOperationSummary(context.Background(), service.ConnectionTraceFilter{Start: now.Add(-2 * time.Hour), End: now})
	require.Equal(t, "unsupported_range", got.Status)
	require.Equal(t, "unknown", got.Coverage)
}
