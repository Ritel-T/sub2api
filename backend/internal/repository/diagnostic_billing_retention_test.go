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

func TestDiagnosticBillingRetentionArchiveIsBoundedAndPreservesExactKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2025, 10, 25, 0, 0, 0, 0, time.UTC)
	// Verify both batches copy request identity/fingerprint before deleting only
	// selected hot keys. No usage, balance, archive TTL or account SQL is allowed.
	query := `WITH victims AS.*SELECT ctid, request_id, api_key_id, request_fingerprint, created_at.*WHERE created_at < \$1.*LIMIT \$2.*INSERT INTO usage_billing_dedup_archive.*SELECT request_id, api_key_id, request_fingerprint, created_at.*DELETE FROM usage_billing_dedup.*WHERE ctid IN`
	mock.ExpectExec(query).WithArgs(cutoff, 10000).WillReturnResult(sqlmock.NewResult(0, 10000))
	mock.ExpectExec(query).WithArgs(cutoff, 10000).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.CleanupUsageBillingDedup(context.Background(), cutoff))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDiagnosticBillingRetentionArchiveFailureStopsFurtherBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	failure := errors.New("archive write unavailable")
	mock.ExpectExec("WITH victims AS").WillReturnError(failure)
	require.ErrorIs(t, newDashboardAggregationRepositoryWithSQL(db).CleanupUsageBillingDedup(context.Background(), time.Now()), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDiagnosticBillingRetentionArchivedRetryNeverChargesAgain(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-request", true: "conflicting-fingerprint"}[conflict], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			cmd := &service.UsageBillingCommand{RequestID: "archived-request", APIKeyID: 7, UserID: 42, BalanceCost: 1.25}
			cmd.Normalize()
			archived := cmd.RequestFingerprint
			if conflict {
				archived = "different"
			}
			mock.ExpectBegin()
			mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup_archive").WithArgs(cmd.RequestID, cmd.APIKeyID).WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(archived))
			mock.ExpectRollback()
			result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			if conflict {
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.False(t, result.Applied)
				require.Nil(t, result.NewBalance)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
