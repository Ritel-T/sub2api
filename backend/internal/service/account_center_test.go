//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type accountCenterUserRepo struct {
	*epochUserRepo
	avatar *UserAvatar
}

func (r *accountCenterUserRepo) GetUserAvatar(context.Context, int64) (*UserAvatar, error) {
	return r.avatar, nil
}

func TestAccountCenterIdentityGenerationAndProjection(t *testing.T) {
	user := &User{ID: 42, Email: "old@example.test", Username: "Name", PasswordHash: "synthetic-hash", Status: StatusActive, Role: RoleAdmin, Balance: 900, TokenVersion: 7}
	repo := &accountCenterUserRepo{epochUserRepo: &epochUserRepo{user: user}, avatar: &UserAvatar{URL: "/api/v1/avatar/synthetic"}}
	settings := &epochSettingsRepo{values: map[string]string{}}
	auth := &AuthService{userRepo: repo, settingService: &SettingService{settingRepo: settings}}
	first, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, "sub2api:42", first.Subject)
	require.True(t, first.Enabled)
	require.Len(t, first.SecurityGeneration, 64)
	require.False(t, first.EmailVerified)
	require.True(t, first.HasAvatar)
	same, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, first.SecurityGeneration, same.SecurityGeneration)
	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	for _, secret := range []string{"synthetic-hash", "PasswordHash", "password", "totp_secret", "balance", "900", "admin"} {
		require.NotContains(t, string(encoded), secret)
	}
	user.Email = "new@example.test"
	email, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, first.Subject, email.Subject)
	require.NotEqual(t, first.SecurityGeneration, email.SecurityGeneration)
	user.PasswordHash = "new-synthetic-hash"
	password, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.NotEqual(t, email.SecurityGeneration, password.SecurityGeneration)
	settings.values[sessionEpochSettingKey(42)] = strings.Repeat("ab", 16)
	epoch, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.NotEqual(t, password.SecurityGeneration, epoch.SecurityGeneration)
	user.TotpEnabled = true
	factor := "synthetic-encrypted-factor"
	user.TotpSecretEncrypted = &factor
	totp, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.NotEqual(t, epoch.SecurityGeneration, totp.SecurityGeneration)
	user.Status = "disabled"
	disabled, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	user.Status = StatusActive
	deleted := time.Now()
	user.DeletedAt = &deleted
	removed, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, removed.Enabled)
}

func TestAccountCenterIdentityFailsClosedWithoutDurableAuthority(t *testing.T) {
	user := &User{ID: 42, Email: "a@example.test", Status: StatusActive, PasswordHash: "synthetic"}
	repo := &accountCenterUserRepo{epochUserRepo: &epochUserRepo{user: user}, avatar: &UserAvatar{URL: "/api/v1/avatar/synthetic"}}
	auth := &AuthService{userRepo: repo}
	_, err := auth.AccountCenterIdentity(context.Background(), 42)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	settings := &epochSettingsRepo{values: map[string]string{}, readErr: errors.New("db failure")}
	auth.settingService = &SettingService{settingRepo: settings}
	_, err = auth.AccountCenterIdentity(context.Background(), 42)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	settings.readErr = nil
	settings.values[sessionEpochSettingKey(42)] = "malformed"
	_, err = auth.AccountCenterIdentity(context.Background(), 42)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	settings.values = map[string]string{}
	_, err = auth.AccountCenterIdentity(context.Background(), 43)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestAccountCenterJWTBindsAuthenticationSnapshot(t *testing.T) {
	ctx := context.Background()
	user := &User{ID: 42, Email: "old@example.test", PasswordHash: "old-hash", Status: StatusActive, Role: RoleUser}
	settings := &epochSettingsRepo{values: map[string]string{}}
	repo := &accountCenterUserRepo{epochUserRepo: &epochUserRepo{user: user}}
	auth := &AuthService{cfg: &config.Config{JWT: config.JWTConfig{Secret: "synthetic-private-key", ExpireHour: 1}, AccountCenter: config.AccountCenterConfig{Enabled: true}}, userRepo: repo, settingService: &SettingService{settingRepo: settings}}
	token, err := auth.GenerateToken(ctx, user)
	require.NoError(t, err)
	claims, err := auth.ValidateToken(token)
	require.NoError(t, err)
	original, err := auth.AccountCenterIdentity(ctx, 42)
	require.NoError(t, err)
	require.Equal(t, original.SecurityGeneration, claims.AccountGeneration)
	secret := "new-encrypted-factor"
	user.TotpEnabled = true
	user.TotpSecretEncrypted = &secret
	changed, err := auth.AccountCenterIdentity(ctx, 42)
	require.NoError(t, err)
	require.NotEqual(t, claims.AccountGeneration, changed.SecurityGeneration)
	// An existing JWT never acquires the newer authority generation.
	still, err := auth.ValidateToken(token)
	require.NoError(t, err)
	require.Equal(t, original.SecurityGeneration, still.AccountGeneration)
	user.PasswordHash = "changed-password"
	changed, err = auth.AccountCenterIdentity(ctx, 42)
	require.NoError(t, err)
	require.NotEqual(t, claims.AccountGeneration, changed.SecurityGeneration)
}
