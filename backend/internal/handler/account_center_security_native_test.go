//go:build unit

package handler

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// This observer exposes synthetic IDs, booleans and counts only. Neither SQL
// query selects credential material, passwords, TOTP secrets or recovery hashes.
type accountCenterNativeMFASummary struct {
	SyntheticOnly              bool                                `json:"syntheticOnly"`
	UserCount                  int                                 `json:"userCount"`
	TotpEnabledCount           int                                 `json:"totpEnabledCount"`
	PasskeyCount               int                                 `json:"passkeyCount"`
	RecoveryCodeCount          int                                 `json:"recoveryCodeCount"`
	RecoveryCodeRemaining      int                                 `json:"recoveryCodeRemaining"`
	RecoveryCodeUsed           int                                 `json:"recoveryCodeUsed"`
	OnlyHashed                 bool                                `json:"onlyHashed"`
	RecoveryCodeOwnershipValid bool                                `json:"recoveryCodeOwnershipValid"`
	PasskeyOwnershipValid      bool                                `json:"passkeyOwnershipValid"`
	Users                      []accountCenterNativeMFAUserSummary `json:"users"`
}

type accountCenterNativeMFAUserSummary struct {
	UserID                int64 `json:"userId"`
	TotpEnabled           bool  `json:"totpEnabled"`
	PasskeyCount          int   `json:"passkeyCount"`
	RecoveryCodeCount     int   `json:"recoveryCodeCount"`
	RecoveryCodeRemaining int   `json:"recoveryCodeRemaining"`
	RecoveryCodeUsed      int   `json:"recoveryCodeUsed"`
}

func accountCenterNativeMFASummaryHandler(db *sql.DB, bridgeSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if bridgeSecret == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+bridgeSecret)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Header("Cache-Control", "no-store")
		summary, err := accountCenterNativeReadMFASummary(c.Request.Context(), db)
		if err != nil {
			// The observer must never return database errors or stored material.
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false})
			return
		}
		c.JSON(http.StatusOK, summary)
	}
}

func accountCenterNativeReadMFASummary(ctx context.Context, db *sql.DB) (*accountCenterNativeMFASummary, error) {
	// Keep aggregate and per-owner counts in the same read-only snapshot while
	// the real handlers may consume or replace a batch concurrently.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	summary := &accountCenterNativeMFASummary{SyntheticOnly: true, Users: []accountCenterNativeMFAUserSummary{}}
	err = tx.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM users),
			(SELECT count(*) FROM users WHERE totp_enabled),
			(SELECT count(*) FROM passkey_credentials),
			(SELECT count(*) FROM account_recovery_codes),
			(SELECT count(*) FROM account_recovery_codes WHERE used_at IS NULL),
			(SELECT count(*) FROM account_recovery_codes WHERE used_at IS NOT NULL),
			NOT EXISTS (SELECT 1 FROM account_recovery_codes WHERE code_hash !~ '^[0-9a-f]{64}$'),
			NOT EXISTS (
				SELECT 1 FROM account_recovery_codes r LEFT JOIN users u ON u.id = r.user_id
				WHERE u.id IS NULL
			),
			NOT EXISTS (
				SELECT 1 FROM passkey_credentials p LEFT JOIN users u ON u.id = p.user_id
				WHERE u.id IS NULL
			)
	`).Scan(&summary.UserCount, &summary.TotpEnabledCount, &summary.PasskeyCount,
		&summary.RecoveryCodeCount, &summary.RecoveryCodeRemaining, &summary.RecoveryCodeUsed,
		&summary.OnlyHashed, &summary.RecoveryCodeOwnershipValid, &summary.PasskeyOwnershipValid)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT u.id, u.totp_enabled,
			(SELECT count(*) FROM passkey_credentials p WHERE p.user_id = u.id),
			(SELECT count(*) FROM account_recovery_codes r WHERE r.user_id = u.id),
			(SELECT count(*) FROM account_recovery_codes r WHERE r.user_id = u.id AND r.used_at IS NULL),
			(SELECT count(*) FROM account_recovery_codes r WHERE r.user_id = u.id AND r.used_at IS NOT NULL)
		FROM users u ORDER BY u.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner accountCenterNativeMFAUserSummary
		if err := rows.Scan(&owner.UserID, &owner.TotpEnabled, &owner.PasskeyCount,
			&owner.RecoveryCodeCount, &owner.RecoveryCodeRemaining, &owner.RecoveryCodeUsed); err != nil {
			return nil, err
		}
		summary.Users = append(summary.Users, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return summary, nil
}

// Unlike the public observer, this seed-only check reads hashes in memory and
// compares them to the real service's display codes. Its result contains no
// material that require assertions could print on a failure.
func accountCenterNativeRecoveryHashesMatch(ctx context.Context, db *sql.DB, userID int64, codes []string) (bool, error) {
	want := make(map[string]bool, len(codes))
	for _, code := range codes {
		hash, err := service.HashAccountRecoveryCode(code)
		if err != nil {
			return false, err
		}
		want[hash] = true
	}
	rows, err := db.QueryContext(ctx, "SELECT code_hash FROM account_recovery_codes WHERE user_id = $1 AND used_at IS NULL", userID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	count, matching := 0, len(want) == service.AccountRecoveryCodeCount
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return false, err
		}
		count++
		matching = matching && want[hash]
	}
	return matching && count == len(want), rows.Err()
}
