package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

const AccountCenterSubjectPrefix = "sub2api:"

// AccountCenterIdentity is an explicit allowlist. No hashes, keys, factors,
// balances or authorization scopes from the business user are serialized.
type AccountCenterIdentity struct {
	Subject            string `json:"subject"`
	UserID             int64  `json:"userId"`
	Enabled            bool   `json:"enabled"`
	SecurityGeneration string `json:"securityGeneration"`
	Name               string `json:"name"`
	HasAvatar          bool   `json:"hasAvatar"`
	Email              string `json:"email"`
	// The existing user row does not prove verified-email status. Never infer it
	// from the address, successful password login, or a current global setting.
	EmailVerified         bool `json:"emailVerified"`
	TotpEnabled           bool `json:"totpEnabled"`
	SessionBindingEnabled bool `json:"sessionBindingEnabled"`
}

func (s *AuthService) AccountCenterIdentity(ctx context.Context, userID int64) (*AccountCenterIdentity, error) {
	if s == nil || s.userRepo == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return nil, ErrServiceUnavailable
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.ID != userID {
		return nil, ErrUserNotFound
	}
	epoch, err := s.sessionEpoch(ctx, userID)
	if err != nil {
		return nil, err
	}
	avatar, err := s.userRepo.GetUserAvatar(ctx, userID)
	if err != nil {
		return nil, ErrServiceUnavailable
	}
	// Missing retains the native default; other read failures must not disable policy.
	binding, err := s.settingService.settingRepo.GetValue(ctx, SettingKeySessionBindingEnabled)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, ErrServiceUnavailable
	}
	generation := accountCenterGeneration(user, epoch)
	return &AccountCenterIdentity{
		Subject: AccountCenterSubjectPrefix + strconv.FormatInt(user.ID, 10), UserID: user.ID,
		Enabled: user.IsActive() && user.DeletedAt == nil, SecurityGeneration: generation,
		Name: user.Username, HasAvatar: avatar != nil && avatar.URL != "", Email: user.Email, TotpEnabled: user.TotpEnabled,
		SessionBindingEnabled: err == nil && binding == "true",
	}, nil
}

// Bind authentication to the user snapshot and epoch used to issue its native JWT.
// Never replace this value with a newer authority snapshot during central login.
func accountCenterGeneration(user *User, epoch string) string {
	factor := ""
	if user.TotpSecretEncrypted != nil {
		factor = *user.TotpSecretEncrypted
	}
	material := fmt.Sprintf("ritel-account-generation-v1\n%d\n%s\n%t\n%s", resolvedTokenVersion(user), epoch, user.TotpEnabled, factor)
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}
