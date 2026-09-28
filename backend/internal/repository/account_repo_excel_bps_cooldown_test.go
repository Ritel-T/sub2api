package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newExcelBPSCooldownMock(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = client.Close()
	})
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func TestExtendExcelBPSRateLimitStoresSafeReasonAndReturnedDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, reason, wantReason string
		previous                 any
	}{
		{"quota", "quota_exhausted", "quota_exhausted", nil},
		{"throttle", "rate_limited", "rate_limited", "2026-09-27T01:00:00Z"},
		{"payload", "upstream payload with private information", "rate_limited", nil},
		{"empty reason", "", "rate_limited", nil},
		{"malformed prior timestamp", "rate_limited", "rate_limited", "not-a-timestamp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newExcelBPSCooldownMock(t)
			until := time.Date(2026, 9, 28, 12, 0, 0, 123456789, time.FixedZone("test", 9*60*60))
			persisted := until.Add(time.Second).UTC()
			mock.ExpectBegin()
			mock.ExpectQuery("(?s)SELECT extra.*FROM accounts.*deleted_at IS NULL FOR UPDATE").WithArgs(int64(27)).
				WillReturnRows(sqlmock.NewRows([]string{"deadline"}).AddRow(tc.previous))
			mock.ExpectQuery("(?s)UPDATE accounts.*COALESCE.*jsonb_build_object.*RETURNING extra").
				WithArgs(int64(27), until.UTC().Format(time.RFC3339Nano), tc.wantReason).
				WillReturnRows(sqlmock.NewRows([]string{"deadline"}).AddRow(persisted.Format(time.RFC3339Nano)))
			mock.ExpectExec("INSERT INTO scheduler_outbox").
				WithArgs(service.SchedulerOutboxEventAccountChanged, int64(27), nil, nil, sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			got, err := repo.ExtendExcelBPSRateLimit(context.Background(), 27, until, tc.reason)
			require.NoError(t, err)
			require.True(t, got.Equal(persisted), "return the database result, not the proposed deadline")
		})
	}
}

func TestExtendExcelBPSRateLimitNeverShortensOrRewritesEqualDeadline(t *testing.T) {
	stored, err := time.Parse(time.RFC3339Nano, "2026-09-28T12:00:00.123456789+09:00")
	require.NoError(t, err)
	for _, delta := range []time.Duration{0, -time.Nanosecond, -time.Hour} {
		t.Run(delta.String(), func(t *testing.T) {
			repo, mock := newExcelBPSCooldownMock(t)
			mock.ExpectBegin()
			mock.ExpectQuery("(?s)SELECT extra.*FOR UPDATE").WithArgs(int64(27)).
				WillReturnRows(sqlmock.NewRows([]string{"deadline"}).AddRow(stored.Format(time.RFC3339Nano)))
			mock.ExpectCommit()
			got, err := repo.ExtendExcelBPSRateLimit(context.Background(), 27, stored.Add(delta).UTC(), "rate_limited")
			require.NoError(t, err)
			require.True(t, got.Equal(stored))
		})
	}
}

func TestExtendExcelBPSRateLimitRollsBackFailures(t *testing.T) {
	for _, stage := range []string{"begin", "select", "update", "outbox", "commit"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newExcelBPSCooldownMock(t)
			failure := errors.New("database unavailable")
			until := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				selectRow := mock.ExpectQuery("(?s)SELECT extra.*FOR UPDATE").WithArgs(int64(27))
				if stage == "select" {
					selectRow.WillReturnError(failure)
				} else {
					selectRow.WillReturnRows(sqlmock.NewRows([]string{"deadline"}).AddRow(nil))
					update := mock.ExpectQuery("(?s)UPDATE accounts.*RETURNING extra").
						WithArgs(int64(27), until.Format(time.RFC3339Nano), "quota_exhausted")
					if stage == "update" {
						update.WillReturnError(failure)
					} else {
						update.WillReturnRows(sqlmock.NewRows([]string{"deadline"}).AddRow(until.Format(time.RFC3339Nano)))
						outbox := mock.ExpectExec("INSERT INTO scheduler_outbox")
						if stage == "outbox" {
							outbox.WillReturnError(failure)
						} else {
							outbox.WillReturnResult(sqlmock.NewResult(0, 1))
						}
					}
				}
				if stage == "commit" {
					mock.ExpectCommit().WillReturnError(failure)
				} else {
					mock.ExpectRollback()
				}
			}
			got, err := repo.ExtendExcelBPSRateLimit(context.Background(), 27, until, "quota_exhausted")
			require.ErrorIs(t, err, failure)
			require.True(t, got.IsZero(), "failed persistence must not report a stored deadline")
		})
	}
}

func TestExtendExcelBPSRateLimitMissingAndInvalidInput(t *testing.T) {
	t.Run("missing or deleted account", func(t *testing.T) {
		repo, mock := newExcelBPSCooldownMock(t)
		mock.ExpectBegin()
		mock.ExpectQuery("(?s)SELECT extra.*deleted_at IS NULL FOR UPDATE").WithArgs(int64(27)).
			WillReturnRows(sqlmock.NewRows([]string{"deadline"}))
		mock.ExpectRollback()
		got, err := repo.ExtendExcelBPSRateLimit(context.Background(), 27, time.Now().Add(time.Hour), "rate_limited")
		require.ErrorIs(t, err, service.ErrAccountNotFound)
		require.True(t, got.IsZero())
	})
	for _, until := range []time.Time{{}, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		t.Run(until.String(), func(t *testing.T) {
			repo, _ := newExcelBPSCooldownMock(t)
			got, err := repo.ExtendExcelBPSRateLimit(context.Background(), 27, until, "rate_limited")
			require.EqualError(t, err, "invalid Excel BPS cooldown deadline")
			require.True(t, got.IsZero())
		})
	}
	t.Run("invalid account ID", func(t *testing.T) {
		repo, _ := newExcelBPSCooldownMock(t)
		got, err := repo.ExtendExcelBPSRateLimit(context.Background(), 0, time.Now().Add(time.Hour), "rate_limited")
		require.ErrorIs(t, err, service.ErrAccountNotFound)
		require.True(t, got.IsZero())
	})
}
