package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"regexp"
	"testing"
	"time"
)

func balanceRecoveryRepoObservation(t *testing.T) (*service.AccountBalanceRecoverySnapshot, service.AccountBalanceRecoveryRequest, []byte) {
	t.Helper()
	now := time.Now().UTC()
	s := &service.AccountBalanceRecoverySnapshot{ID: 194, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusError, UpdatedAt: now, Schedulable: false, ErrorMessage: "Payment required (402): insufficient balance", Credentials: map[string]any{"api_key": "fake"}, Extra: map[string]any{"quality_state": "preserve"}}
	p, err := service.BuildAccountBalanceRecoveryPreview(s, now)
	require.NoError(t, err)
	target := false
	req := service.AccountBalanceRecoveryRequest{ExpectedUpdatedAt: p.ExpectedUpdatedAt, ExpectedFingerprint: p.ExpectedFingerprint, TargetSchedulable: &target, FundingConfirmed: true, Confirm: true}
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	return s, req, raw
}
func expectBalanceRecoveryLock(m sqlmock.Sqlmock, raw []byte, pending bool) {
	m.ExpectQuery(regexp.QuoteMeta(balanceRecoverySnapshotSQL + " FOR UPDATE")).WithArgs(int64(194)).WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow(raw))
	m.ExpectQuery("SELECT EXISTS.*account_schedulable_timers.*pending").WithArgs(int64(194)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(pending))
}
func TestBalanceRecoveryRepositoryUpdatesOnlyRecoveryColumns(t *testing.T) {
	r, m := newExcelBPSCooldownMock(t)
	_, req, raw := balanceRecoveryRepoObservation(t)
	stored := time.Now().UTC()
	m.ExpectBegin()
	expectBalanceRecoveryLock(m, raw, false)
	m.ExpectQuery(regexp.QuoteMeta("UPDATE accounts SET status = 'active', error_message = NULL, schedulable = $2, updated_at = NOW()\nWHERE id = $1 AND deleted_at IS NULL RETURNING updated_at")).WithArgs(int64(194), false).WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(stored))
	m.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(194), nil, nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	got, err := r.RecoverAccountBalanceIfObserved(context.Background(), 194, req)
	require.NoError(t, err)
	require.False(t, got.Schedulable)
	require.True(t, got.IndependentLimitsPreserved)
	require.True(t, stored.Equal(got.UpdatedAt))
	require.Equal(t, "owner_confirmed", got.FundingEvidence)
}
func TestBalanceRecoveryRepositoryRollsBackChangedOrPending(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed identity", true: "pending timer"}[pending], func(t *testing.T) {
			r, m := newExcelBPSCooldownMock(t)
			s, req, raw := balanceRecoveryRepoObservation(t)
			if !pending {
				s.Credentials["api_key"] = "changed"
				var err error
				raw, err = json.Marshal(s)
				require.NoError(t, err)
			}
			m.ExpectBegin()
			expectBalanceRecoveryLock(m, raw, pending)
			m.ExpectRollback()
			got, err := r.RecoverAccountBalanceIfObserved(context.Background(), 194, req)
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}
func TestBalanceRecoveryRepositoryOutboxFailureRollsBack(t *testing.T) {
	r, m := newExcelBPSCooldownMock(t)
	_, req, raw := balanceRecoveryRepoObservation(t)
	m.ExpectBegin()
	expectBalanceRecoveryLock(m, raw, false)
	m.ExpectQuery("UPDATE accounts SET status = 'active', error_message = NULL, schedulable").WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
	failure := errors.New("outbox unavailable")
	m.ExpectExec("INSERT INTO scheduler_outbox").WillReturnError(failure)
	m.ExpectRollback()
	got, err := r.RecoverAccountBalanceIfObserved(context.Background(), 194, req)
	require.ErrorIs(t, err, failure)
	require.Nil(t, got)
}
