package service

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

// AuthSessionEpochSettingPrefix contains internal revocation state, never
// exported through public/admin settings responses. No credential is stored.
const AuthSessionEpochSettingPrefix = "auth_session_epoch:"
const sessionEpochDBTimeout = 2 * time.Second

func sessionEpochSettingKey(userID int64) string {
	return AuthSessionEpochSettingPrefix + strconv.FormatInt(userID, 10)
}

func (s *AuthService) sessionEpoch(ctx context.Context, userID int64) (string, error) {
	if s.settingService == nil || s.settingService.settingRepo == nil {
		// Supports token-only integrations. Revoke-all refuses to claim success
		// without the durable store.
		return "", nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, sessionEpochDBTimeout)
	defer cancel()
	value, err := s.settingService.settingRepo.GetValue(dbCtx, sessionEpochSettingKey(userID))
	if errors.Is(err, ErrSettingNotFound) {
		return "", nil // Existing JWTs remain valid until explicitly revoked.
	}
	if err != nil {
		return "", ErrServiceUnavailable
	}
	if decoded, err := hex.DecodeString(value); err != nil || len(decoded) != 16 {
		return "", ErrServiceUnavailable
	}
	return value, nil
}

func (s *AuthService) validateSessionEpoch(ctx context.Context, userID int64, epoch string) error {
	current, err := s.sessionEpoch(ctx, userID)
	if err != nil {
		return err
	}
	if current != epoch {
		return ErrTokenRevoked
	}
	return nil
}
