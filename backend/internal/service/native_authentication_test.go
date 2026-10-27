//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNativeAuthenticationProofPreservedAcrossRefresh(t *testing.T) {
	user := &User{ID: 42, Email: "proof@example.test", PasswordHash: "synthetic", Status: StatusActive, Role: RoleUser}
	repo := &epochUserRepo{user: user}
	settings := &epochSettingsRepo{values: map[string]string{}}
	cache := &epochRefreshCache{tokens: map[string]*RefreshTokenData{}}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "synthetic-test-key", ExpireHour: 1, RefreshTokenExpireDays: 31}, AccountCenter: config.AccountCenterConfig{Enabled: true}}
	auth := &AuthService{cfg: cfg, userRepo: repo, refreshTokenCache: cache, settingService: &SettingService{settingRepo: settings}}
	when := time.Now().Add(-31 * time.Minute).Unix()
	ctx := WithNativeAuthenticationProof(context.Background(), NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: when})
	pair, err := auth.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	claims, err := auth.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	require.Equal(t, when, claims.AuthenticationTime)
	require.Equal(t, []string{"pwd", "otp"}, claims.AuthenticationMethods)
	rotated, err := auth.RefreshTokenPair(context.Background(), pair.RefreshToken)
	require.NoError(t, err)
	after, err := auth.ValidateToken(rotated.AccessToken)
	require.NoError(t, err)
	require.Equal(t, when, after.AuthenticationTime)
	require.Equal(t, claims.AuthenticationMethods, after.AuthenticationMethods)
	_, err = auth.RequireRecentAccountAuthentication(context.Background(), rotated.AccessToken, user.ID, true)
	require.Error(t, err)
	fresh, err := auth.GenerateTokenPair(WithNativeAuthenticationProof(context.Background(), NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: time.Now().Unix()}), user, "")
	require.NoError(t, err)
	_, err = auth.RequireRecentAccountAuthentication(context.Background(), fresh.AccessToken, user.ID, true)
	require.NoError(t, err)
	secret := "new-factor"
	user.TotpEnabled = true
	user.TotpSecretEncrypted = &secret
	_, err = auth.RequireRecentAccountAuthentication(context.Background(), fresh.AccessToken, user.ID, true)
	require.ErrorIs(t, err, ErrTokenRevoked)
}
func TestNativeAuthenticationProofCannotUpgradeAnOldGeneration(t *testing.T) {
	user := &User{ID: 42, Email: "proof@example.test", PasswordHash: "old", Status: StatusActive}
	settings := &epochSettingsRepo{values: map[string]string{}}
	auth := &AuthService{cfg: &config.Config{JWT: config.JWTConfig{Secret: "synthetic", ExpireHour: 1}, AccountCenter: config.AccountCenterConfig{Enabled: true}}, userRepo: &epochUserRepo{user: user}, settingService: &SettingService{settingRepo: settings}}
	old := accountCenterGeneration(user, "")
	user.PasswordHash = "new"
	_, err := auth.GenerateToken(WithNativeAuthenticationProof(context.Background(), NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: time.Now().Unix(), Generation: old}), user)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestRecoveryCodesSurvivePasswordEmailChangesButNotAuthenticatorReplacement(t *testing.T) {
	enabled := time.Now()
	user := &User{ID: 42, Email: "old@example.test", PasswordHash: "old-hash", TotpEnabled: true, TotpEnabledAt: &enabled}
	original := accountRecoveryFactorGeneration(user)
	oldAuthentication := accountCenterGeneration(user, "")
	user.Email = "new@example.test"
	user.PasswordHash = "new-hash"
	require.Equal(t, original, accountRecoveryFactorGeneration(user))
	require.NotEqual(t, oldAuthentication, accountCenterGeneration(user, ""))
	newer := enabled.Add(time.Second)
	user.TotpEnabledAt = &newer
	require.NotEqual(t, original, accountRecoveryFactorGeneration(user))
}

func TestAccountSecurityResetNoteAuditRetainsMethodAndMasksCredentialShapes(t *testing.T) {
	require.Equal(t, "manual video callback and original receipt checked", AccountSecurityResetAuditNote("manual video callback and original receipt checked"))
	code := "ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567"
	note := AccountSecurityResetAuditNote("checked owner recovery=" + code + " password=private-pass token=rt_0123456789012345678901234567890123456789")
	require.NotContains(t, note, code)
	require.NotContains(t, note, "private-pass")
	require.NotContains(t, note, "rt_012345")
	require.Contains(t, note, "checked owner")
}

func TestAccountSecurityResetAuditNormalizesAllAcceptedRecoverySeparators(t *testing.T) {
	code := "ABCD - EFGH - IJKL - MNOP - QRST - UVWX - YZ23 - 4567"
	note := AccountSecurityResetAuditNote("manual callback checked; code " + code)
	require.NotContains(t, note, "ABCD")
	require.NotContains(t, note, "4567")
	require.Contains(t, note, "manual callback checked")
	compact := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	require.NotContains(t, AccountSecurityResetAuditNote("核实方式：回访。恢复凭据 "+compact), compact)
}
