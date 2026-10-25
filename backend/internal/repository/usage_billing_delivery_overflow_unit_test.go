//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Check the persisted notification snapshot, including the recovered routing
// evidence and the executed (eight-place) charge, rather than just its INSERT.
type durableOverflowPayloadArgument struct {
	command service.UsageBillingCommand
	balance float64
}

func (a durableOverflowPayloadArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var payload service.SubscriptionOverflowNotificationPayload
	return json.Unmarshal([]byte(raw), &payload) == nil &&
		reflect.DeepEqual(payload.SubscriptionOverflowBillingEvidence, *a.command.SubscriptionOverflow) &&
		payload.UserID == a.command.UserID && payload.BalanceCost == a.command.BalanceCost &&
		payload.BalanceAfter == a.balance && !payload.ChargedAt.IsZero()
}

func durableOverflowUnitEvent(t *testing.T) (service.UsageBillingCommand, []byte, []byte, float64) {
	t.Helper()
	raw := 0.000078125
	cmd := service.UsageBillingCommand{
		RequestID: "durable-overflow", APIKeyID: 7, UserID: 42, AccountID: 3,
		BalanceCost: raw, QuotedActualCost: &raw,
		SubscriptionOverflow: &service.SubscriptionOverflowBillingEvidence{
			SubscriptionID: 3, SourceGroupID: 1, TargetGroupID: 2,
			Dimension: "daily", WindowStart: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
			DailyUsageUSD: 10, DailyLimitUSD: 10, WeeklyUsageUSD: 20, WeeklyLimitUSD: 70,
			MonthlyUsageUSD: 30, MonthlyLimitUSD: 300, RateMultiplier: 0.6,
		},
	}
	cmd.Normalize()
	payload, err := json.Marshal(service.UsageBillingEvent{Version: 1, Command: cmd, Usage: service.UsageLog{
		RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID, UserID: cmd.UserID, AccountID: cmd.AccountID,
		Model: "mock", GroupID: &cmd.SubscriptionOverflow.TargetGroupID,
		ActualCost: cmd.BalanceCost, CreatedAt: cmd.SubscriptionOverflow.WindowStart.Add(time.Hour),
	}})
	require.NoError(t, err)
	balance := 99.99992187
	result, err := json.Marshal(service.UsageBillingApplyResult{Applied: true, NewBalance: &balance})
	require.NoError(t, err)
	return cmd, payload, result, balance
}

func expectDurableOverflowEvent(mock sqlmock.Sqlmock, cmd service.UsageBillingCommand, payload, result []byte, state string) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT schema_version,payload,state,result,request_fingerprint").
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"schema_version", "payload", "state", "result", "request_fingerprint"}).
			AddRow(1, payload, state, result, cmd.RequestFingerprint))
}

func expectDurableOverflowCharge(mock sqlmock.Sqlmock, cmd service.UsageBillingCommand, balance float64) {
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").
		WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup_archive").
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}))
	mock.ExpectQuery("SELECT EXISTS.*FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(conditionalBalanceDeductSQL).WithArgs(cmd.BalanceCost, cmd.UserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(balance))
}

func expectDurableOverflowEnqueue(mock sqlmock.Sqlmock, cmd service.UsageBillingCommand, balance float64) *sqlmock.ExpectedExec {
	e := cmd.SubscriptionOverflow
	return mock.ExpectExec("INSERT INTO subscription_overflow_email_outbox.*ON CONFLICT.*DO NOTHING").
		WithArgs(cmd.UserID, e.SubscriptionID, e.Dimension, e.WindowStart.UTC(),
			durableOverflowPayloadArgument{command: cmd, balance: balance}, e.SourceGroupID, e.TargetGroupID)
}

func expectDurableOverflowQuote(mock sqlmock.Sqlmock, cmd service.UsageBillingCommand) *sqlmock.ExpectedExec {
	return mock.ExpectExec("INSERT INTO usage_billing_quotes").
		WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, "0.000078125", cmd.BalanceCost)
}

func TestUsageBillingDeliveryOverflowPersistsNoticeAndNeverReplaysIt(t *testing.T) {
	for _, lostCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "lost_commit_response"}[lostCommit], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			cmd, payload, storedResult, balance := durableOverflowUnitEvent(t)
			repo := &usageBillingRepository{db: db}
			key := service.UsageBillingEventKey{RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID}
			expectDurableOverflowEvent(mock, cmd, payload, nil, "pending")
			expectDurableOverflowCharge(mock, cmd, balance)
			expectDurableOverflowEnqueue(mock, cmd, balance).WillReturnResult(sqlmock.NewResult(1, 1))
			expectDurableOverflowQuote(mock, cmd).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO usage_logs").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE usage_billing_events SET state='applied'").WillReturnResult(sqlmock.NewResult(0, 1))
			commit := mock.ExpectCommit()
			if lostCommit {
				commit.WillReturnError(errors.New("lost commit response"))
				mock.ExpectQuery("SELECT state,result FROM usage_billing_events").
					WillReturnRows(sqlmock.NewRows([]string{"state", "result"}).AddRow("applied", storedResult))
			}
			recovered, err := repo.ApplyStagedUsageBilling(context.Background(), key)
			require.NoError(t, err)
			require.True(t, recovered.Result.Applied)
			require.True(t, recovered.NeedsCache)
			require.Equal(t, cmd.SubscriptionOverflow, recovered.Event.Command.SubscriptionOverflow)
			// Neither cache recovery nor a later completed replay may charge or
			// enqueue again; no monetary/outbox SQL is expected on either retry.
			for _, state := range []string{"applied", "complete"} {
				expectDurableOverflowEvent(mock, cmd, payload, storedResult, state)
				mock.ExpectRollback()
				replayed, err := repo.ApplyStagedUsageBilling(context.Background(), key)
				require.NoError(t, err)
				require.False(t, replayed.Result.Applied)
				require.Equal(t, state == "applied", replayed.NeedsCache)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageBillingDeliveryOverflowFailureRollsBackAndRetryEnqueues(t *testing.T) {
	for _, failedStep := range []string{"outbox", "quote", "usage"} {
		t.Run(failedStep, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			cmd, payload, _, balance := durableOverflowUnitEvent(t)
			repo := &usageBillingRepository{db: db}
			key := service.UsageBillingEventKey{RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID}
			failure := errors.New("injected " + failedStep + " failure")
			for attempt := 0; attempt < 2; attempt++ {
				expectDurableOverflowEvent(mock, cmd, payload, nil, "pending")
				expectDurableOverflowCharge(mock, cmd, balance)
				for _, step := range []string{"outbox", "quote", "usage"} {
					var expectation *sqlmock.ExpectedExec
					switch step {
					case "outbox":
						expectation = expectDurableOverflowEnqueue(mock, cmd, balance)
					case "quote":
						expectation = expectDurableOverflowQuote(mock, cmd)
					case "usage":
						expectation = mock.ExpectExec("INSERT INTO usage_logs")
					}
					if attempt == 0 && step == failedStep {
						expectation.WillReturnError(failure)
						break
					}
					expectation.WillReturnResult(sqlmock.NewResult(1, 1))
				}
				if attempt == 0 {
					mock.ExpectRollback()
					mock.ExpectExec("UPDATE usage_billing_events SET state=.*failure_code=").
						WithArgs(cmd.RequestID, cmd.APIKeyID, "pending", "apply_failed").
						WillReturnResult(sqlmock.NewResult(0, 1))
				} else {
					mock.ExpectExec("UPDATE usage_billing_events SET state='applied'").WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				}
				recovered, err := repo.ApplyStagedUsageBilling(context.Background(), key)
				if attempt == 0 {
					require.ErrorIs(t, err, failure)
					require.Nil(t, recovered)
				} else {
					require.NoError(t, err)
					require.True(t, recovered.Result.Applied)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageBillingDeliveryOverflowHistoricalClaimNeverEnqueues(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(map[bool]string{false: "hot_claim", true: "archive_claim"}[archived], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			cmd, payload, _, _ := durableOverflowUnitEvent(t)
			expectDurableOverflowEvent(mock, cmd, payload, nil, "pending")
			claim := mock.ExpectQuery("INSERT INTO usage_billing_dedup")
			if archived {
				claim.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
				mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup_archive").
					WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
			} else {
				claim.WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery("SELECT request_fingerprint.*FROM usage_billing_dedup").
					WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
			}
			mock.ExpectExec("UPDATE usage_billing_events SET state='legacy'").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			recovered, err := (&usageBillingRepository{db: db}).ApplyStagedUsageBilling(context.Background(),
				service.UsageBillingEventKey{RequestID: cmd.RequestID, APIKeyID: cmd.APIKeyID})
			require.NoError(t, err)
			require.False(t, recovered.Result.Applied)
			require.False(t, recovered.NeedsCache)
			require.NoError(t, mock.ExpectationsWereMet(), "historical claims must not synthesize a notice or a new charge")
		})
	}
}
