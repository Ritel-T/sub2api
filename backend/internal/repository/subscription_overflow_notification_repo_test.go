//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func overflowBillingTestCommand() *service.UsageBillingCommand {
	return &service.UsageBillingCommand{RequestID: "overflow-request", APIKeyID: 7, UserID: 42, BalanceCost: 1.25, SubscriptionOverflow: &service.SubscriptionOverflowBillingEvidence{SubscriptionID: 3, SourceGroupID: 1, TargetGroupID: 2, Dimension: "daily", WindowStart: time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC), DailyUsageUSD: 10, DailyLimitUSD: 10, RateMultiplier: 0.6}}
}

func TestSubscriptionOverflowOutboxIsAtomicWithSuccessfulBilling(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "outbox_failure_rolls_back_charge"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			expectDeletedKeySettlementPrefix(mock)
			expect := mock.ExpectExec("INSERT INTO subscription_overflow_email_outbox.*ON CONFLICT.*DO NOTHING")
			failure := errors.New("outbox unavailable")
			if fail {
				expect.WillReturnError(failure)
				mock.ExpectRollback()
			} else {
				expect.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}
			result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), overflowBillingTestCommand())
			if fail {
				require.ErrorIs(t, err, failure)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.True(t, result.Applied)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSubscriptionOverflowOutboxDoesNotEnqueueWithoutCharge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cmd := overflowBillingTestCommand()
	cmd.BalanceCost = 0
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup_archive").WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}))
	mock.ExpectCommit()
	result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverflowOutboxClaimNeverRequeuesExpiredSending(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Now().UTC()
	mock.ExpectExec("SET status='unknown'.*WHERE status='sending'").WithArgs(now, now.Add(-10*time.Minute)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("WITH candidate.*FOR UPDATE SKIP LOCKED.*SET status='sending'").WithArgs(now, sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
	job, err := NewSubscriptionOverflowNotificationRepository(db).Claim(context.Background(), now)
	require.NoError(t, err)
	require.Nil(t, job)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionOverflowOutboxCompletionRequiresClaimOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectExec("UPDATE subscription_overflow_email_outbox.*WHERE id=\\$1 AND claim_token=\\$2 AND status='sending'").WithArgs(int64(1), "old-token", "sent", "", nil).WillReturnResult(sqlmock.NewResult(0, 0))
	err = NewSubscriptionOverflowNotificationRepository(db).Complete(context.Background(), 1, "old-token", "sent", "", time.Time{})
	require.ErrorContains(t, err, "no longer owned")
	require.NoError(t, mock.ExpectationsWereMet())
}
