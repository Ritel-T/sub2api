package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

func (s *AuthService) SetAccountRecoveryCodeRepository(repo AccountRecoveryCodeRepository) {
	s.recoveryCodes = repo
}
func (s *AuthService) ReplaceAccountRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	if !s.AccountCenterEnabled() || s.recoveryCodes == nil {
		return nil, ErrServiceUnavailable
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive() || !user.TotpEnabled {
		return nil, infraerrors.Forbidden("RECOVERY_DISABLED", "enable the authenticator first")
	}
	epoch, err := s.sessionEpoch(ctx, userID)
	if err != nil {
		return nil, err
	}
	authenticationGeneration := accountCenterGeneration(user, epoch)
	generation := accountRecoveryFactorGeneration(user)
	codes, hashes, err := GenerateAccountRecoveryCodes()
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	if err = s.recoveryCodes.Replace(ctx, userID, generation, hashes); err != nil {
		return nil, ErrServiceUnavailable
	}
	identity, err := s.AccountCenterIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !identity.Enabled || identity.SecurityGeneration != authenticationGeneration {
		return nil, ErrTokenRevoked
	}
	return codes, nil
}
func (s *AuthService) RemainingAccountRecoveryCodes(ctx context.Context, userID int64) (int, error) {
	if !s.AccountCenterEnabled() || s.recoveryCodes == nil {
		return 0, ErrServiceUnavailable
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	if !user.TotpEnabled {
		return 0, nil
	}
	epoch, err := s.sessionEpoch(ctx, userID)
	if err != nil {
		return 0, err
	}
	_ = epoch
	n, err := s.recoveryCodes.Remaining(ctx, userID, accountRecoveryFactorGeneration(user))
	if err != nil {
		return 0, ErrServiceUnavailable
	}
	return n, nil
}
func (s *AuthService) ConsumeAccountRecoveryCode(ctx context.Context, userID int64, expectedGeneration, code string) error {
	if !s.AccountCenterEnabled() || s.recoveryCodes == nil {
		return ErrServiceUnavailable
	}
	identity, err := s.AccountCenterIdentity(ctx, userID)
	if err != nil {
		return err
	}
	if !identity.Enabled || identity.SecurityGeneration != expectedGeneration {
		return ErrTokenRevoked
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.TotpEnabled {
		return ErrTotpNotEnabled
	}
	hash, err := HashAccountRecoveryCode(code)
	if err != nil {
		return infraerrors.Unauthorized("RECOVERY_CODE_INVALID", "recovery code is invalid or already used")
	}
	used, err := s.recoveryCodes.Consume(ctx, userID, accountRecoveryFactorGeneration(user), hash)
	if err != nil {
		return ErrServiceUnavailable
	}
	if !used {
		return infraerrors.Unauthorized("RECOVERY_CODE_INVALID", "recovery code is invalid or already used")
	}
	return nil
}
func (s *AuthService) InvalidateAccountRecoveryCodes(ctx context.Context, userID int64) error {
	if s.recoveryCodes == nil {
		return ErrServiceUnavailable
	}
	if err := s.recoveryCodes.Invalidate(ctx, userID); err != nil {
		return ErrServiceUnavailable
	}
	return nil
}

// Backup codes belong to the authenticator enrollment, not a password/email or
// browser session. Disabling/resetting the factor deletes the set; re-enrollment
// replaces it. Password/email changes must not silently destroy a saved backup.
func accountRecoveryFactorGeneration(user *User) string {
	enrollment := ""
	if user.TotpEnabledAt != nil {
		enrollment = user.TotpEnabledAt.UTC().Format(time.RFC3339Nano)
	} else if user.TotpSecretEncrypted != nil {
		enrollment = *user.TotpSecretEncrypted
	}
	material := fmt.Sprintf("ritel-recovery-factor-v1\n%d\n%t\n%s", user.ID, user.TotpEnabled, enrollment)
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}
