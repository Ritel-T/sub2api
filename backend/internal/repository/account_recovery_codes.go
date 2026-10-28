package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountRecoveryCodeRepository struct{ db *sql.DB }

var _ service.AccountRecoveryCodeRepository = (*accountRecoveryCodeRepository)(nil)

func NewAccountRecoveryCodeRepository(db *sql.DB) service.AccountRecoveryCodeRepository {
	return &accountRecoveryCodeRepository{db: db}
}

// Hash the namespaced, full-width user ID to avoid a 32-bit ID truncation or a
// dependency on existing rows. Collisions can only add harmless serialization.
const accountRecoveryCodeLockSQL = "SELECT pg_advisory_xact_lock(hashtextextended('sub2api:account_recovery_codes:' || $1::text, 0))"
const accountRecoveryCodeDeleteSQL = "DELETE FROM account_recovery_codes WHERE user_id = $1"
const accountRecoveryCodeConsumeSQL = "UPDATE account_recovery_codes SET used_at = clock_timestamp() WHERE user_id = $1 AND generation = $2 AND code_hash = $3 AND used_at IS NULL"
const accountRecoveryCodeRemainingSQL = "SELECT COUNT(*) FROM account_recovery_codes WHERE user_id = $1 AND generation = $2 AND used_at IS NULL"

func (r *accountRecoveryCodeRepository) Replace(ctx context.Context, userID int64, generation string, hashes []string) error {
	if !validAccountRecoveryCodeGeneration(generation) || len(hashes) != service.AccountRecoveryCodeCount {
		return service.ErrInvalidAccountRecoveryCodeInput
	}
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		if !validAccountRecoveryCodeHash(hash) {
			return service.ErrInvalidAccountRecoveryCodeInput
		}
		if _, exists := seen[hash]; exists {
			return service.ErrInvalidAccountRecoveryCodeInput
		}
		seen[hash] = struct{}{}
	}
	return r.withUserTransaction(ctx, userID, func(tx *sql.Tx) error {
		if err := deleteAccountRecoveryCodes(ctx, tx, userID); err != nil {
			return err
		}
		// One parameterized batch insert; any failure rolls back the old-set delete.
		var query strings.Builder
		_, _ = query.WriteString("INSERT INTO account_recovery_codes (user_id, generation, code_hash) VALUES ")
		args := make([]any, 0, len(hashes)*3)
		for i, hash := range hashes {
			if i > 0 {
				_, _ = query.WriteString(", ")
			}
			start := i*3 + 1
			fmt.Fprintf(&query, "($%d, $%d, $%d)", start, start+1, start+2)
			args = append(args, userID, generation, hash)
		}
		result, err := tx.ExecContext(ctx, query.String(), args...)
		if err != nil {
			return accountRecoveryCodeStorageError(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return accountRecoveryCodeStorageError(err)
		}
		if affected != int64(len(hashes)) {
			return service.ErrAccountRecoveryCodeStorage
		}
		return nil
	})
}

func (r *accountRecoveryCodeRepository) Consume(ctx context.Context, userID int64, generation string, hash string) (bool, error) {
	if !validAccountRecoveryCodeGeneration(generation) || !validAccountRecoveryCodeHash(hash) {
		return false, service.ErrInvalidAccountRecoveryCodeInput
	}
	consumed := false
	err := r.withUserTransaction(ctx, userID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, accountRecoveryCodeConsumeSQL, userID, generation, hash)
		if err != nil {
			return accountRecoveryCodeStorageError(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return accountRecoveryCodeStorageError(err)
		}
		if affected < 0 || affected > 1 {
			return service.ErrAccountRecoveryCodeStorage
		}
		consumed = affected == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return consumed, nil
}

func (r *accountRecoveryCodeRepository) Remaining(ctx context.Context, userID int64, generation string) (int, error) {
	if !validAccountRecoveryCodeGeneration(generation) {
		return 0, service.ErrInvalidAccountRecoveryCodeInput
	}
	var remaining int
	err := r.withUserTransaction(ctx, userID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, accountRecoveryCodeRemainingSQL, userID, generation).Scan(&remaining); err != nil {
			return accountRecoveryCodeStorageError(err)
		}
		if remaining < 0 || remaining > service.AccountRecoveryCodeCount {
			return service.ErrAccountRecoveryCodeStorage
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return remaining, nil
}

func (r *accountRecoveryCodeRepository) Invalidate(ctx context.Context, userID int64) error {
	return r.withUserTransaction(ctx, userID, func(tx *sql.Tx) error {
		return deleteAccountRecoveryCodes(ctx, tx, userID)
	})
}

func deleteAccountRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) error {
	result, err := tx.ExecContext(ctx, accountRecoveryCodeDeleteSQL, userID)
	if err != nil {
		return accountRecoveryCodeStorageError(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return accountRecoveryCodeStorageError(err)
	}
	if affected < 0 {
		return service.ErrAccountRecoveryCodeStorage
	}
	return nil
}

func (r *accountRecoveryCodeRepository) withUserTransaction(ctx context.Context, userID int64, action func(*sql.Tx) error) error {
	if ctx == nil || userID <= 0 {
		return service.ErrInvalidAccountRecoveryCodeInput
	}
	if r == nil || r.db == nil {
		return service.ErrAccountRecoveryCodeStorage
	}
	// READ COMMITTED ensures the statement after a waiting lock sees the committed
	// replacement, even if the database's configured default isolation is stronger.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return accountRecoveryCodeStorageError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, accountRecoveryCodeLockSQL, userID); err != nil {
		return accountRecoveryCodeStorageError(err)
	}
	if err := action(tx); err != nil {
		return err
	}
	return accountRecoveryCodeStorageError(tx.Commit())
}

func accountRecoveryCodeStorageError(err error) error {
	if err == nil {
		return nil
	}
	// Preserve only known-safe context errors, never SQL/driver messages, query
	// parameters, connection strings, or credentials (including through Unwrap).
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return service.ErrAccountRecoveryCodeStorage
}

func validAccountRecoveryCodeGeneration(generation string) bool {
	if len(generation) == 0 || len(generation) > 128 {
		return false
	}
	for i := 0; i < len(generation); i++ {
		if generation[i] < '!' || generation[i] > '~' {
			return false
		}
	}
	return true
}

func validAccountRecoveryCodeHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
