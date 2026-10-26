package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountTotpAtomicEnabler = (*userRepository)(nil)

const accountFactorUserLockSQL = `SELECT email, password_hash, status, totp_enabled, totp_secret_encrypted, deleted_at FROM users WHERE id = $1 FOR UPDATE`
const accountFactorEpochSQL = `SELECT value FROM settings WHERE key = $1`
const accountFactorEnableTotpSQL = `UPDATE users SET totp_secret_encrypted = $2, totp_enabled = TRUE, totp_enabled_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`

// Lock the original user row before reading the epoch, matching admin reset's
// factor-write order. Keep this lock until the factor write commits: if reset
// wins first its new generation is observed; otherwise reset clears our write.
func lockAccountFactorGeneration(ctx context.Context, tx *sql.Tx, userID int64, proof service.NativeAuthenticationProof) error {
	if proof.Generation == "" {
		return service.ErrTokenRevoked
	}
	// Original users have no token_version column. Leave TokenVersion at zero
	// and unresolved so the service derives the fingerprint from raw email/hash.
	user := &service.User{ID: userID}
	if err := tx.QueryRowContext(ctx, accountFactorUserLockSQL, userID).Scan(
		&user.Email, &user.PasswordHash, &user.Status, &user.TotpEnabled,
		&user.TotpSecretEncrypted, &user.DeletedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrUserNotFound
		}
		return fmt.Errorf("lock account factor snapshot: %w", err)
	}
	if !user.IsActive() || user.DeletedAt != nil {
		return service.ErrUserNotActive
	}
	var epoch string
	key := service.AuthSessionEpochSettingPrefix + strconv.FormatInt(userID, 10)
	err := tx.QueryRowContext(ctx, accountFactorEpochSQL, key).Scan(&epoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read account factor epoch: %w", err)
	}
	if err == nil {
		if decoded, err := hex.DecodeString(epoch); err != nil || len(decoded) != 16 {
			return service.ErrServiceUnavailable
		}
	}
	if service.AccountFactorGeneration(user, epoch) != proof.Generation {
		return service.ErrTokenRevoked
	}
	return nil
}

// EnableTotpWithSecret changes the encrypted secret and enabled state together,
// holding the original user lock through generation validation and commit.
func (r *userRepository) EnableTotpWithSecret(ctx context.Context, userID int64, encryptedSecret string, proof service.NativeAuthenticationProof) error {
	if r == nil {
		return service.ErrServiceUnavailable
	}
	db, ok := r.sql.(*sql.DB)
	if !ok || db == nil {
		return service.ErrServiceUnavailable
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin account totp enable: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockAccountFactorGeneration(ctx, tx, userID, proof); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, accountFactorEnableTotpSQL, userID, encryptedSecret)
	if err != nil {
		return fmt.Errorf("enable account totp: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("enable account totp rows: %w", err)
	}
	if affected == 0 {
		return service.ErrUserNotFound
	}
	if affected != 1 {
		return service.ErrServiceUnavailable
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit account totp enable: %w", err)
	}
	return nil
}
