package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountSecurityResetRepository = (*accountRecoveryCodeRepository)(nil)

const accountSecurityResetUserLockSQL = "SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE"
const accountSecurityResetTotpSQL = "UPDATE users SET totp_enabled = FALSE, totp_secret_encrypted = NULL, totp_enabled_at = NULL, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL"
const accountSecurityResetPasskeysSQL = "DELETE FROM passkey_credentials WHERE user_id = $1"
const accountSecurityResetEpochSQL = "INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at"

// ResetAccountSecurity shares exactly the recovery-code lock namespace and lock
// order. Every durable mutation, including the session fence, has one commit.
func (r *accountRecoveryCodeRepository) ResetAccountSecurity(ctx context.Context, targetID int64, newEpoch string) error {
	decoded, err := hex.DecodeString(newEpoch)
	if ctx == nil || targetID <= 0 || err != nil || len(decoded) != 16 || newEpoch != hex.EncodeToString(decoded) {
		return service.ErrAccountSecurityResetInput
	}
	if r == nil || r.db == nil {
		return service.ErrAccountSecurityResetStorage
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return service.ErrAccountSecurityResetStorage
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, accountRecoveryCodeLockSQL, targetID); err != nil {
		return service.ErrAccountSecurityResetStorage
	}
	var lockedID int64
	if err = tx.QueryRowContext(ctx, accountSecurityResetUserLockSQL, targetID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrUserNotFound
		}
		return service.ErrAccountSecurityResetStorage
	}
	if lockedID != targetID {
		return service.ErrAccountSecurityResetStorage
	}
	result, err := tx.ExecContext(ctx, accountSecurityResetTotpSQL, targetID)
	if err != nil {
		return service.ErrAccountSecurityResetStorage
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return service.ErrAccountSecurityResetStorage
	}
	for _, query := range []string{accountSecurityResetPasskeysSQL, accountRecoveryCodeDeleteSQL} {
		result, err = tx.ExecContext(ctx, query, targetID)
		if err != nil {
			return service.ErrAccountSecurityResetStorage
		}
		if affected, err = result.RowsAffected(); err != nil || affected < 0 {
			return service.ErrAccountSecurityResetStorage
		}
	}
	key := service.AuthSessionEpochSettingPrefix + strconv.FormatInt(targetID, 10)
	result, err = tx.ExecContext(ctx, accountSecurityResetEpochSQL, key, newEpoch)
	if err != nil {
		return service.ErrAccountSecurityResetStorage
	}
	if affected, err = result.RowsAffected(); err != nil || affected != 1 {
		return service.ErrAccountSecurityResetStorage
	}
	// A driver/network error from COMMIT can mean the transaction committed.
	// Never label that ambiguity a known failure or automatically retry it.
	if err = tx.Commit(); err != nil {
		return service.ErrAccountSecurityResetUnknown
	}
	return nil
}
