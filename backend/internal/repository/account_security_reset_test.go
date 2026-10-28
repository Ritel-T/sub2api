package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Literal SQL expectations reject writes to email, password, UID, status,
// balances, API keys, token_version, and the stable passkey user handle.
const resetTestUserSQL = "SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE"
const resetTestTotpSQL = "UPDATE users SET totp_enabled = FALSE, totp_secret_encrypted = NULL, totp_enabled_at = NULL, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL"
const resetTestPasskeysSQL = "DELETE FROM passkey_credentials WHERE user_id = $1"
const resetTestEpochSQL = "INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at"

func newResetSQLMock(t *testing.T) (service.AccountSecurityResetRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	// The original constructor must expose the optional reset capability.
	repo, ok := NewAccountRecoveryCodeRepository(db).(service.AccountSecurityResetRepository)
	require.True(t, ok)
	return repo, mock
}

func TestAccountSecurityResetAtomicExactScope(t *testing.T) {
	for _, id := range []int64{42, 4294967338} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			repo, mock := newResetSQLMock(t)
			epoch := strings.Repeat("a1", 16)
			mock.ExpectBegin()
			mock.ExpectExec(recoveryTestLockSQL).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(resetTestUserSQL).WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
			mock.ExpectExec(resetTestTotpSQL).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(resetTestPasskeysSQL).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 3))
			mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 8))
			mock.ExpectExec(resetTestEpochSQL).WithArgs(fmt.Sprintf("auth_session_epoch:%d", id), epoch).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			require.NoError(t, repo.ResetAccountSecurity(context.Background(), id, epoch))
		})
	}
}

func TestAccountSecurityResetRollsBackEveryFailedStatement(t *testing.T) {
	for fail := 0; fail < 6; fail++ {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			repo, mock := newResetSQLMock(t)
			secretErr := errors.New("driver error with private material")
			mock.ExpectBegin()
			lock := mock.ExpectExec(recoveryTestLockSQL).WithArgs(int64(42))
			if fail == 0 {
				lock.WillReturnError(secretErr)
			} else {
				lock.WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if fail >= 1 {
				q := mock.ExpectQuery(resetTestUserSQL).WithArgs(int64(42))
				if fail == 1 {
					q.WillReturnError(secretErr)
				} else {
					q.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
				}
			}
			for i, query := range []string{resetTestTotpSQL, resetTestPasskeysSQL, recoveryTestDeleteSQL, resetTestEpochSQL} {
				step := i + 2
				if fail < step {
					break
				}
				e := mock.ExpectExec(query)
				if i == 3 {
					e.WithArgs("auth_session_epoch:42", strings.Repeat("ab", 16))
				} else {
					e.WithArgs(int64(42))
				}
				if fail == step {
					e.WillReturnError(secretErr)
				} else {
					e.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			mock.ExpectRollback()
			err := repo.ResetAccountSecurity(context.Background(), 42, strings.Repeat("ab", 16))
			require.ErrorIs(t, err, service.ErrAccountSecurityResetStorage)
			require.NotContains(t, err.Error(), "private material")
			require.NotErrorIs(t, err, secretErr)
		})
	}
}

func TestAccountSecurityResetMissingUserAndUnexpectedUpdateCount(t *testing.T) {
	for _, mode := range []string{"missing", "query_error", "wrong_id", "zero_update", "multiple_update", "rows_error"} {
		t.Run(mode, func(t *testing.T) {
			repo, mock := newResetSQLMock(t)
			mock.ExpectBegin()
			mock.ExpectExec(recoveryTestLockSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
			q := mock.ExpectQuery(resetTestUserSQL).WithArgs(int64(42))
			switch mode {
			case "missing":
				q.WillReturnRows(sqlmock.NewRows([]string{"id"}))
			case "query_error":
				q.WillReturnError(errors.New("db"))
			case "wrong_id":
				q.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(43))
			default:
				q.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
				e := mock.ExpectExec(resetTestTotpSQL).WithArgs(int64(42))
				switch mode {
				case "zero_update":
					e.WillReturnResult(sqlmock.NewResult(0, 0))
				case "multiple_update":
					e.WillReturnResult(sqlmock.NewResult(0, 2))
				case "rows_error":
					e.WillReturnResult(sqlmock.NewErrorResult(errors.New("rows")))
				}
			}
			mock.ExpectRollback()
			err := repo.ResetAccountSecurity(context.Background(), 42, strings.Repeat("ab", 16))
			if mode == "missing" {
				require.ErrorIs(t, err, service.ErrUserNotFound)
			} else {
				require.ErrorIs(t, err, service.ErrAccountSecurityResetStorage)
			}
		})
	}
}

func TestAccountSecurityResetBeginCommitAndInvalidInputs(t *testing.T) {
	t.Run("begin", func(t *testing.T) {
		repo, mock := newResetSQLMock(t)
		mock.ExpectBegin().WillReturnError(errors.New("db"))
		require.ErrorIs(t, repo.ResetAccountSecurity(context.Background(), 42, strings.Repeat("ab", 16)), service.ErrAccountSecurityResetStorage)
	})
	t.Run("commit_unknown", func(t *testing.T) {
		repo, mock := newResetSQLMock(t)
		mock.ExpectBegin()
		mock.ExpectExec(recoveryTestLockSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(resetTestUserSQL).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
		for _, query := range []string{resetTestTotpSQL, resetTestPasskeysSQL, recoveryTestDeleteSQL} {
			mock.ExpectExec(query).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectExec(resetTestEpochSQL).WithArgs("auth_session_epoch:42", strings.Repeat("ab", 16)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit().WillReturnError(errors.New("connection lost"))
		require.ErrorIs(t, repo.ResetAccountSecurity(context.Background(), 42, strings.Repeat("ab", 16)), service.ErrAccountSecurityResetUnknown)
	})
	t.Run("invalid", func(t *testing.T) {
		repo, _ := newResetSQLMock(t)
		for _, epoch := range []string{"", "ab", strings.Repeat("AB", 16), strings.Repeat("zz", 16), strings.Repeat("ab", 17)} {
			require.ErrorIs(t, repo.ResetAccountSecurity(context.Background(), 42, epoch), service.ErrAccountSecurityResetInput)
		}
		require.ErrorIs(t, repo.ResetAccountSecurity(context.Background(), 0, strings.Repeat("ab", 16)), service.ErrAccountSecurityResetInput)
		//lint:ignore SA1012 Verify nil context is rejected before database access.
		require.ErrorIs(t, repo.ResetAccountSecurity(nil, 42, strings.Repeat("ab", 16)), service.ErrAccountSecurityResetInput) //nolint:staticcheck // SA1012: intentionally test nil-context rejection before database access.
		var nilRepo *accountRecoveryCodeRepository
		require.ErrorIs(t, nilRepo.ResetAccountSecurity(context.Background(), 42, strings.Repeat("ab", 16)), service.ErrAccountSecurityResetStorage)
	})
}
