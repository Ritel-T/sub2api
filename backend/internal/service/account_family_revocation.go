package service

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
)

// Keep family fences under the existing internal prefix so settings exports
// exclude them just like user epochs. Fences never expire or rotate on refresh.
func accountFamilyRevocationSettingKey(userID int64, sessionID string) (string, error) {
	if userID <= 0 || (len(sessionID) != 16 && len(sessionID) != 32) {
		return "", ErrInvalidToken
	}
	if _, err := hex.DecodeString(sessionID); err != nil {
		return "", ErrInvalidToken
	}
	return AuthSessionEpochSettingPrefix + "family:" + strconv.FormatInt(userID, 10) + ":" + sessionID, nil
}

func (s *AuthService) validateAccountSessionFamily(ctx context.Context, userID int64, sessionID, generation string) error {
	// Legacy sessions and disabled integrations retain their native behavior.
	if !s.AccountCenterEnabled() || generation == "" {
		return nil
	}
	key, err := accountFamilyRevocationSettingKey(userID, sessionID)
	if err != nil {
		return err
	}
	if s.settingService == nil || s.settingService.settingRepo == nil {
		return ErrServiceUnavailable
	}
	dbCtx, cancel := context.WithTimeout(ctx, sessionEpochDBTimeout)
	defer cancel()
	_, err = s.settingService.settingRepo.GetValue(dbCtx, key)
	if errors.Is(err, ErrSettingNotFound) {
		return nil
	}
	if err != nil {
		return ErrServiceUnavailable
	}
	// Any persisted entry is a fence, including an unexpected/empty value.
	return ErrTokenRevoked
}

// RevokeAccountSessionFamily authenticates the native session without requiring
// recent authentication, then fences only its signed family before Redis cleanup.
// userID and sessionID must come from the existing JWT middleware, never JSON.
func (s *AuthService) RevokeAccountSessionFamily(ctx context.Context, userID int64, sessionID, nativeToken string) error {
	if err := s.requireAccountCenterEnabled(); err != nil {
		return err
	}
	if s.userRepo == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return ErrServiceUnavailable
	}
	claims, err := s.ValidateTokenContext(ctx, nativeToken)
	if err != nil {
		return err
	}
	if claims.UserID != userID || claims.SessionID != sessionID || claims.AccountGeneration == "" {
		return ErrInvalidToken
	}
	key, err := accountFamilyRevocationSettingKey(claims.UserID, claims.SessionID)
	if err != nil {
		return err
	}
	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.ID != claims.UserID {
		return ErrUserNotFound
	}
	if !user.IsActive() || user.DeletedAt != nil {
		return ErrUserNotActive
	}
	if claims.TokenVersion != resolvedTokenVersion(user) || claims.AccountGeneration != accountCenterGeneration(user, claims.SessionEpoch) {
		return ErrTokenRevoked
	}

	dbCtx, cancel := context.WithTimeout(ctx, sessionEpochDBTimeout)
	defer cancel()
	if err := s.settingService.settingRepo.Set(dbCtx, key, "revoked"); err != nil {
		// A failed write response can still mean the commit happened. Never retry
		// or report successful logout on the caller's behalf.
		return ErrServiceUnavailable.WithMetadata(map[string]string{"outcome": "unknown", "stage": "persist_family_revocation"})
	}
	if s.refreshTokenCache == nil {
		return ErrServiceUnavailable.WithMetadata(map[string]string{"outcome": "unknown", "stage": "refresh_cleanup", "family_revoked": "true"})
	}
	if err := s.refreshTokenCache.DeleteTokenFamily(ctx, claims.SessionID); err != nil {
		return ErrServiceUnavailable.WithMetadata(map[string]string{"outcome": "unknown", "stage": "refresh_cleanup", "family_revoked": "true"})
	}
	return nil
}
