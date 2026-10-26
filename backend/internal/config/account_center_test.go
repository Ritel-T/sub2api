package config

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAccountCenterConfig(t *testing.T) {
	require.NoError(t, (AccountCenterConfig{}).Validate())
	for _, secret := range []string{"", strings.Repeat("a", 42), strings.Repeat("a", 43) + "\n", strings.Repeat("a", 257), strings.Repeat("密", 43)} {
		require.Error(t, (AccountCenterConfig{Enabled: true, BridgeSecret: secret, PublicOrigin: "https://account.ritel.si"}).Validate())
	}
	require.Error(t, (AccountCenterConfig{Enabled: true, BridgeSecret: strings.Repeat("a", 43)}).Validate())
	require.NoError(t, (AccountCenterConfig{Enabled: true, BridgeSecret: strings.Repeat("a", 43), PublicOrigin: "https://account.ritel.si"}).Validate())
	cfg := Config{AccountCenter: AccountCenterConfig{Enabled: true, PublicOrigin: "https://account.ritel.si"}, JWT: JWTConfig{RefreshTokenExpireDays: 31}, Totp: TotpConfig{EncryptionKey: strings.Repeat("a", 64), EncryptionKeyConfigured: true}, WebAuthn: WebAuthnConfig{Enabled: true, RPID: "account.ritel.si", RPOrigins: []string{"https://account.ritel.si"}}}
	require.NoError(t, cfg.ValidateAccountCenterRuntime())
	cfg.JWT.RefreshTokenExpireDays = 30
	require.Error(t, cfg.ValidateAccountCenterRuntime())
	cfg.JWT.RefreshTokenExpireDays = 31
	cfg.Totp.EncryptionKeyConfigured = false
	require.Error(t, cfg.ValidateAccountCenterRuntime())
	cfg.Totp.EncryptionKeyConfigured = true
	cfg.WebAuthn.RPID = "ritel.si"
	require.Error(t, cfg.ValidateAccountCenterRuntime())
	cfg.WebAuthn.RPID = "account.ritel.si"
	cfg.Turnstile.StrictContext = true
	require.Error(t, cfg.ValidateAccountCenterRuntime())
	cfg.Turnstile.ExpectedHostnames = []string{"account.ritel.si"}
	require.NoError(t, cfg.ValidateAccountCenterRuntime())
	cfg.AccountCenter.Enabled = false
	require.NoError(t, cfg.ValidateAccountCenterRuntime())

}
