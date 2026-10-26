package service

import (
	"context"
	"github.com/pquerna/otp/totp"
	"time"
)

// Reuses the original user, encryptor and Redis setup/enable flow. A signed
// recent native authentication substitutes for resubmitting a password/email code.
func (s *TotpService) BeginAccountSetup(ctx context.Context, auth *AuthService, userID int64, nativeToken string) (*TotpSetupResponse, error) {
	if _, err := auth.RequireRecentAccountAuthentication(ctx, nativeToken, userID, false); err != nil {
		return nil, err
	}
	if !s.settingService.IsTotpEnabled(ctx) {
		return nil, ErrTotpNotEnabled
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TotpEnabled {
		return nil, ErrTotpAlreadyEnabled
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: user.Email})
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	token, err := generateRandomToken(32)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	if err = s.cache.SetSetupSession(ctx, userID, &TotpSetupSession{Secret: key.Secret(), SetupToken: token, CreatedAt: time.Now()}, totpSetupTTL); err != nil {
		return nil, ErrServiceUnavailable
	}
	return &TotpSetupResponse{Secret: key.Secret(), QRCodeURL: key.URL(), SetupToken: token, Countdown: int(totpSetupTTL.Seconds())}, nil
}

func (s *TotpService) DisableAccountTotp(ctx context.Context, auth *AuthService, userID int64, nativeToken string) error {
	if _, err := auth.RequireRecentAccountAuthentication(ctx, nativeToken, userID, true); err != nil {
		return err
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.TotpEnabled {
		return ErrTotpNotSetup
	}
	return s.userRepo.DisableTotp(ctx, userID)
}
func (s *TotpService) CheckAccountRecoveryAttempt(ctx context.Context, userID int64) error {
	n, err := s.cache.IncrementVerifyAttempts(ctx, userID)
	if err != nil {
		return ErrServiceUnavailable
	}
	if n > maxTotpAttempts {
		return ErrTotpTooManyAttempts
	}
	return nil
}

func (s *TotpService) ClearAccountRecoveryAttempts(ctx context.Context, userID int64) error {
	return s.cache.ClearVerifyAttempts(ctx, userID)
}
