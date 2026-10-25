package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticRetentionManagedNeverDeletes(t *testing.T) {
	t.Setenv("SUB2API_DIAGNOSTIC_ARCHIVE_MANAGED", "true")
	// A nil DB makes any attempted SQL panic: managed mode must not depend on
	// archive freshness, parent usage existence, or even database availability.
	repo := &usageLogRepository{}
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.cleanupRequestTimings(context.Background()))
		require.NoError(t, repo.cleanupConnectionTraces(context.Background()))
	}
	require.Equal(t, 3, diagnosticRetentionDays())
}

func TestDiagnosticRetentionDefaultKeepsBoundedTTLAndOrphanCleanup(t *testing.T) {
	for _, value := range []string{"", "false", "1", "TRUE", "typo"} {
		t.Run("env="+value, func(t *testing.T) {
			t.Setenv("SUB2API_DIAGNOSTIC_ARCHIVE_MANAGED", value)
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := newUsageLogRepositoryWithSQL(nil, db)
			mock.ExpectExec(`DELETE FROM request_timing_details.*30 days.*OR NOT EXISTS.*LIMIT 10000`).WillReturnResult(sqlmock.NewResult(0, 7))
			mock.ExpectExec(`DELETE FROM request_connection_traces.*30 days.*LIMIT 10000`).WillReturnResult(sqlmock.NewResult(0, 8))
			require.NoError(t, repo.cleanupRequestTimings(context.Background()))
			require.NoError(t, repo.cleanupConnectionTraces(context.Background()))
			require.Equal(t, 30, diagnosticRetentionDays())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestDiagnosticRetentionDefaultReturnsFailures(t *testing.T) {
	t.Setenv("SUB2API_DIAGNOSTIC_ARCHIVE_MANAGED", "false")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	failure := errors.New("database unavailable")
	mock.ExpectExec("DELETE FROM request_timing_details").WillReturnError(failure)
	mock.ExpectExec("DELETE FROM request_connection_traces").WillReturnError(failure)
	require.ErrorIs(t, repo.cleanupRequestTimings(context.Background()), failure)
	require.ErrorIs(t, repo.cleanupConnectionTraces(context.Background()), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDiagnosticRetentionListMetadata(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "archive-managed"}[managed], func(t *testing.T) {
			t.Setenv("SUB2API_DIAGNOSTIC_ARCHIVE_MANAGED", map[bool]string{false: "false", true: "true"}[managed])
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := newUsageLogRepositoryWithSQL(nil, db)
			now := time.Now()
			mock.ExpectQuery("WITH source AS").WithArgs(now.Add(-time.Hour), now, 21, 0).WillReturnRows(sqlmock.NewRows([]string{"trace_id"}))
			page, err := repo.ListConnectionTraces(context.Background(), service.ConnectionTraceFilter{Start: now.Add(-time.Hour), End: now, Page: 1, PageSize: 20})
			require.NoError(t, err)
			if managed {
				require.Equal(t, 3, page.RetentionDays)
				require.True(t, page.ArchiveManaged)
				require.Equal(t, 3, page.HotRetentionTargetDays)
			} else {
				require.Equal(t, 30, page.RetentionDays)
				require.False(t, page.ArchiveManaged)
				require.Zero(t, page.HotRetentionTargetDays)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
