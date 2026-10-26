package repository

import (
	"context"
	"database/sql/driver"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const recoveryTestLockSQL = "SELECT pg_advisory_xact_lock(hashtextextended('sub2api:account_recovery_codes:' || $1::text, 0))"
const recoveryTestDeleteSQL = "DELETE FROM account_recovery_codes WHERE user_id = $1"
const recoveryTestConsumeSQL = "UPDATE account_recovery_codes SET used_at = clock_timestamp() WHERE user_id = $1 AND generation = $2 AND code_hash = $3 AND used_at IS NULL"
const recoveryTestRemainingSQL = "SELECT COUNT(*) FROM account_recovery_codes WHERE user_id = $1 AND generation = $2 AND used_at IS NULL"
const recoveryTestInsertSQL = "INSERT INTO account_recovery_codes (user_id, generation, code_hash) VALUES ($1, $2, $3), ($4, $5, $6), ($7, $8, $9), ($10, $11, $12), ($13, $14, $15), ($16, $17, $18), ($19, $20, $21), ($22, $23, $24)"

func newRecoveryCodeSQLMock(t *testing.T) (service.AccountRecoveryCodeRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	return NewAccountRecoveryCodeRepository(db), mock
}

func recoveryTestHashes() []string {
	hashes := make([]string, service.AccountRecoveryCodeCount)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%064x", i+1)
	}
	return hashes
}

func recoveryTestInsertArgs(userID int64, generation string, hashes []string) []driver.Value {
	args := make([]driver.Value, 0, len(hashes)*3)
	for _, hash := range hashes {
		args = append(args, userID, generation, hash)
	}
	return args
}

func expectRecoveryCodeLock(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectBegin()
	mock.ExpectExec(recoveryTestLockSQL).WithArgs(userID).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestAccountRecoveryCodeReplaceAtomicBatch(t *testing.T) {
	repo, mock := newRecoveryCodeSQLMock(t)
	hashes := recoveryTestHashes()
	expectRecoveryCodeLock(mock, 42)
	mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(recoveryTestInsertSQL).WithArgs(recoveryTestInsertArgs(42, "new-generation", hashes)...).WillReturnResult(sqlmock.NewResult(0, 8))
	mock.ExpectCommit()
	require.NoError(t, repo.Replace(context.Background(), 42, "new-generation", hashes))
}

func TestAccountRecoveryCodeConsumeOnceAndExactScope(t *testing.T) {
	repo, mock := newRecoveryCodeSQLMock(t)
	hash := recoveryTestHashes()[0]
	for _, tc := range []struct {
		name             string
		user             int64
		generation, hash string
		affected         int64
	}{
		{"first use", 42, "g1", hash, 1},
		{"replay", 42, "g1", hash, 0},
		{"other user", 43, "g1", hash, 0},
		{"other generation", 42, "g2", hash, 0},
		{"other hash", 42, "g1", strings.Repeat("f", 64), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expectRecoveryCodeLock(mock, tc.user)
			mock.ExpectExec(recoveryTestConsumeSQL).WithArgs(tc.user, tc.generation, tc.hash).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			mock.ExpectCommit()
			consumed, err := repo.Consume(context.Background(), tc.user, tc.generation, tc.hash)
			require.NoError(t, err)
			require.Equal(t, tc.affected == 1, consumed)
		})
	}
}

func TestAccountRecoveryCodeReplacementRetainsGenerationFence(t *testing.T) {
	repo, mock := newRecoveryCodeSQLMock(t)
	hashes := recoveryTestHashes()
	expectRecoveryCodeLock(mock, 42)
	mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 8))
	mock.ExpectExec(recoveryTestInsertSQL).WithArgs(recoveryTestInsertArgs(42, "new", hashes)...).WillReturnResult(sqlmock.NewResult(0, 8))
	mock.ExpectCommit()
	require.NoError(t, repo.Replace(context.Background(), 42, "new", hashes))
	// Even a deliberately reused hash must not match a late old-generation request.
	expectRecoveryCodeLock(mock, 42)
	mock.ExpectExec(recoveryTestConsumeSQL).WithArgs(int64(42), "old", hashes[0]).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	consumed, err := repo.Consume(context.Background(), 42, "old", hashes[0])
	require.NoError(t, err)
	require.False(t, consumed)
}

func TestAccountRecoveryCodeRemainingGenerationAndUnusedScope(t *testing.T) {
	for _, count := range []int{0, 3, 8} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			repo, mock := newRecoveryCodeSQLMock(t)
			expectRecoveryCodeLock(mock, 42)
			mock.ExpectQuery(recoveryTestRemainingSQL).WithArgs(int64(42), "g1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
			mock.ExpectCommit()
			remaining, err := repo.Remaining(context.Background(), 42, "g1")
			require.NoError(t, err)
			require.Equal(t, count, remaining)
		})
	}
}

func TestAccountRecoveryCodeInvalidateAllGenerationsIdempotent(t *testing.T) {
	repo, mock := newRecoveryCodeSQLMock(t)
	for _, affected := range []int64{8, 0} {
		expectRecoveryCodeLock(mock, 42)
		mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, affected))
		mock.ExpectCommit()
		require.NoError(t, repo.Invalidate(context.Background(), 42))
	}
}

func TestAccountRecoveryCodeTransactionFailuresSanitized(t *testing.T) {
	secretErr := errors.New("postgres://admin:secret@database.invalid/recovery: SQL parameter confidential-hash")
	hashes := recoveryTestHashes()
	for _, operation := range []string{"replace", "consume", "remaining", "invalidate"} {
		for _, phase := range []string{"begin", "lock", "statement", "commit"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				repo, mock := newRecoveryCodeSQLMock(t)
				if phase == "begin" {
					mock.ExpectBegin().WillReturnError(secretErr)
				} else if phase == "lock" {
					mock.ExpectBegin()
					mock.ExpectExec(recoveryTestLockSQL).WithArgs(int64(42)).WillReturnError(secretErr)
					mock.ExpectRollback()
				} else {
					expectRecoveryCodeLock(mock, 42)
					switch operation {
					case "replace":
						mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 8))
						insert := mock.ExpectExec(recoveryTestInsertSQL).WithArgs(recoveryTestInsertArgs(42, "g1", hashes)...)
						if phase == "statement" {
							insert.WillReturnError(secretErr)
						} else {
							insert.WillReturnResult(sqlmock.NewResult(0, 8))
						}
					case "consume":
						update := mock.ExpectExec(recoveryTestConsumeSQL).WithArgs(int64(42), "g1", hashes[0])
						if phase == "statement" {
							update.WillReturnError(secretErr)
						} else {
							update.WillReturnResult(sqlmock.NewResult(0, 1))
						}
					case "remaining":
						query := mock.ExpectQuery(recoveryTestRemainingSQL).WithArgs(int64(42), "g1")
						if phase == "statement" {
							query.WillReturnError(secretErr)
						} else {
							query.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(8))
						}
					case "invalidate":
						deletion := mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42))
						if phase == "statement" {
							deletion.WillReturnError(secretErr)
						} else {
							deletion.WillReturnResult(sqlmock.NewResult(0, 8))
						}
					}
					if phase == "statement" {
						mock.ExpectRollback()
					} else {
						mock.ExpectCommit().WillReturnError(secretErr)
					}
				}
				var err error
				switch operation {
				case "replace":
					err = repo.Replace(context.Background(), 42, "g1", hashes)
				case "consume":
					var consumed bool
					consumed, err = repo.Consume(context.Background(), 42, "g1", hashes[0])
					require.False(t, consumed, "no success before a confirmed commit")
				case "remaining":
					var remaining int
					remaining, err = repo.Remaining(context.Background(), 42, "g1")
					require.Zero(t, remaining)
				case "invalidate":
					err = repo.Invalidate(context.Background(), 42)
				}
				require.ErrorIs(t, err, service.ErrAccountRecoveryCodeStorage)
				require.NotContains(t, err.Error(), "secret")
				require.NotContains(t, err.Error(), "confidential-hash")
				require.False(t, errors.Is(err, secretErr), "driver errors must not leak through Unwrap")
			})
		}
	}
}

func TestAccountRecoveryCodeReplaceDeleteFailureRollsBack(t *testing.T) {
	repo, mock := newRecoveryCodeSQLMock(t)
	expectRecoveryCodeLock(mock, 42)
	mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnError(errors.New("driver details"))
	mock.ExpectRollback()
	require.ErrorIs(t, repo.Replace(context.Background(), 42, "g1", recoveryTestHashes()), service.ErrAccountRecoveryCodeStorage)
}

func TestAccountRecoveryCodeAffectedRowsFailuresRollBack(t *testing.T) {
	for _, operation := range []string{"replace insert", "replace delete", "consume", "invalidate"} {
		for _, resultName := range []string{"rows error", "invalid count"} {
			t.Run(operation+"/"+resultName, func(t *testing.T) {
				repo, mock := newRecoveryCodeSQLMock(t)
				expectRecoveryCodeLock(mock, 42)
				var result driver.Result = sqlmock.NewErrorResult(errors.New("sensitive result error"))
				if resultName == "invalid count" {
					affected := int64(-1)
					if operation == "replace insert" {
						affected = 7
					}
					if operation == "consume" {
						affected = 2
					}
					result = sqlmock.NewResult(0, affected)
				}
				hashes := recoveryTestHashes()
				switch operation {
				case "replace insert":
					mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnResult(sqlmock.NewResult(0, 8))
					mock.ExpectExec(recoveryTestInsertSQL).WithArgs(recoveryTestInsertArgs(42, "g1", hashes)...).WillReturnResult(result)
				case "replace delete", "invalidate":
					mock.ExpectExec(recoveryTestDeleteSQL).WithArgs(int64(42)).WillReturnResult(result)
				case "consume":
					mock.ExpectExec(recoveryTestConsumeSQL).WithArgs(int64(42), "g1", hashes[0]).WillReturnResult(result)
				}
				mock.ExpectRollback()
				var err error
				switch operation {
				case "replace insert", "replace delete":
					err = repo.Replace(context.Background(), 42, "g1", hashes)
				case "invalidate":
					err = repo.Invalidate(context.Background(), 42)
				case "consume":
					var consumed bool
					consumed, err = repo.Consume(context.Background(), 42, "g1", hashes[0])
					require.False(t, consumed)
				}
				require.ErrorIs(t, err, service.ErrAccountRecoveryCodeStorage)
			})
		}
	}
}

func TestAccountRecoveryCodeRemainingInvalidResultRollsBack(t *testing.T) {
	for _, value := range []any{-1, 9, "not-a-count", nil} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			repo, mock := newRecoveryCodeSQLMock(t)
			expectRecoveryCodeLock(mock, 42)
			mock.ExpectQuery(recoveryTestRemainingSQL).WithArgs(int64(42), "g1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(value))
			mock.ExpectRollback()
			remaining, err := repo.Remaining(context.Background(), 42, "g1")
			require.ErrorIs(t, err, service.ErrAccountRecoveryCodeStorage)
			require.Zero(t, remaining)
		})
	}
}

func TestAccountRecoveryCodeInvalidInputsDoNotOpenTransaction(t *testing.T) {
	repo, _ := newRecoveryCodeSQLMock(t)
	ctx := context.Background()
	hashes := recoveryTestHashes()
	for _, generation := range []string{"", " ", " g1", "g1 ", "g\n1", "g\x001", "日本語", strings.Repeat("g", 129)} {
		require.ErrorIs(t, repo.Replace(ctx, 42, generation, hashes), service.ErrInvalidAccountRecoveryCodeInput)
		consumed, err := repo.Consume(ctx, 42, generation, hashes[0])
		require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
		require.False(t, consumed)
		remaining, err := repo.Remaining(ctx, 42, generation)
		require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
		require.Zero(t, remaining)
	}
	for _, userID := range []int64{0, -1} {
		require.ErrorIs(t, repo.Replace(ctx, userID, "g1", hashes), service.ErrInvalidAccountRecoveryCodeInput)
		consumed, err := repo.Consume(ctx, userID, "g1", hashes[0])
		require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
		require.False(t, consumed)
		remaining, err := repo.Remaining(ctx, userID, "g1")
		require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
		require.Zero(t, remaining)
		require.ErrorIs(t, repo.Invalidate(ctx, userID), service.ErrInvalidAccountRecoveryCodeInput)
	}
	for _, hash := range []string{"", "plaintext-recovery-code", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		invalid := append([]string(nil), hashes...)
		invalid[0] = hash
		require.ErrorIs(t, repo.Replace(ctx, 42, "g1", invalid), service.ErrInvalidAccountRecoveryCodeInput)
		consumed, err := repo.Consume(ctx, 42, "g1", hash)
		require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
		require.False(t, consumed)
	}
	duplicate := append([]string(nil), hashes...)
	duplicate[7] = duplicate[0]
	for _, invalid := range [][]string{nil, {}, hashes[:7], append(append([]string(nil), hashes...), strings.Repeat("f", 64)), duplicate} {
		require.ErrorIs(t, repo.Replace(ctx, 42, "g1", invalid), service.ErrInvalidAccountRecoveryCodeInput)
	}
	require.ErrorIs(t, repo.Replace(nil, 42, "g1", hashes), service.ErrInvalidAccountRecoveryCodeInput)
	_, err := repo.Consume(nil, 42, "g1", hashes[0])
	require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
	_, err = repo.Remaining(nil, 42, "g1")
	require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCodeInput)
	require.ErrorIs(t, repo.Invalidate(nil, 42), service.ErrInvalidAccountRecoveryCodeInput)
}

func TestAccountRecoveryCodeNilDatabaseFailsClosed(t *testing.T) {
	repo := NewAccountRecoveryCodeRepository(nil)
	ctx := context.Background()
	hashes := recoveryTestHashes()
	require.ErrorIs(t, repo.Replace(ctx, 42, "g1", hashes), service.ErrAccountRecoveryCodeStorage)
	consumed, err := repo.Consume(ctx, 42, "g1", hashes[0])
	require.False(t, consumed)
	require.ErrorIs(t, err, service.ErrAccountRecoveryCodeStorage)
	remaining, err := repo.Remaining(ctx, 42, "g1")
	require.Zero(t, remaining)
	require.ErrorIs(t, err, service.ErrAccountRecoveryCodeStorage)
	require.ErrorIs(t, repo.Invalidate(ctx, 42), service.ErrAccountRecoveryCodeStorage)
}

func TestAccountRecoveryCodeContextErrorsRemainSafe(t *testing.T) {
	for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
		repo, mock := newRecoveryCodeSQLMock(t)
		mock.ExpectBegin().WillReturnError(fmt.Errorf("sensitive driver wrapper: %w", ctxErr))
		err := repo.Invalidate(context.Background(), 42)
		require.True(t, err == ctxErr, "return only the safe context sentinel")
	}
}

func TestAccountRecoveryCodeGenerateEntropyAndHashes(t *testing.T) {
	require.Equal(t, 8, service.AccountRecoveryCodeCount)
	require.GreaterOrEqual(t, service.AccountRecoveryCodeEntropyBytes*8, 128)
	allCodes := make(map[string]bool)
	allHashes := make(map[string]bool)
	displayPattern := regexp.MustCompile("^[A-Z2-7]{4}(-[A-Z2-7]{4}){7}$")
	for range 2 {
		codes, hashes, err := service.GenerateAccountRecoveryCodes()
		require.NoError(t, err)
		require.Len(t, codes, 8)
		require.Len(t, hashes, 8)
		for i, code := range codes {
			require.Regexp(t, displayPattern, code)
			compact := strings.ReplaceAll(code, "-", "")
			entropy, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(compact)
			require.NoError(t, err)
			require.Len(t, entropy, 20, "each generated code encodes 160 bits")
			require.False(t, allCodes[compact], "codes must not repeat across sets")
			allCodes[compact] = true
			hash, err := service.HashAccountRecoveryCode(code)
			require.NoError(t, err)
			require.Equal(t, hash, hashes[i])
			decodedHash, err := hex.DecodeString(hash)
			require.NoError(t, err)
			require.Len(t, decodedHash, 32)
			require.NotEqual(t, compact, hash)
			require.False(t, allHashes[hash])
			allHashes[hash] = true
		}
	}
}

func TestAccountRecoveryCodeHashNormalizationAndInvalidInput(t *testing.T) {
	compact := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	expected, err := service.HashAccountRecoveryCode(compact)
	require.NoError(t, err)
	require.Equal(t, "49484a568d540b2b41605d9b938d54289075412d594494f79d46c0d18e748b5e", expected, "stable domain-separated SHA-256 vector")
	for _, input := range []string{
		strings.ToLower(compact),
		"ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567",
		"  abcd efgh ijkl mnop qrst uvwx yz23 4567\r\n",
		"\tabcd-efgh\nijkl-mnop-qrst-uvwx-yz23-4567\t",
	} {
		hash, err := service.HashAccountRecoveryCode(input)
		require.NoError(t, err)
		require.Equal(t, expected, hash)
	}
	changed, err := service.HashAccountRecoveryCode("BBCDEFGHIJKLMNOPQRSTUVWXYZ234567")
	require.NoError(t, err)
	require.NotEqual(t, expected, changed)
	for _, input := range []string{
		"", "---- \t\n", compact[:31], compact + "A", "0" + compact[1:], "1" + compact[1:], "8" + compact[1:], "9" + compact[1:],
		"=" + compact[1:], compact + "=", "/" + compact[1:], "Ａ" + compact[1:], "é" + compact[1:], "\x00" + compact,
		"\u00a0" + compact, strings.Repeat(" ", 129) + compact,
	} {
		hash, err := service.HashAccountRecoveryCode(input)
		require.ErrorIs(t, err, service.ErrInvalidAccountRecoveryCode)
		require.Empty(t, hash)
	}
}
