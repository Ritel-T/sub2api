//go:build unit

package service

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type resetServiceUserRepo struct {
	UserRepository
	users map[int64]*User
}

func (r *resetServiceUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	u := r.users[id]
	if u == nil {
		return nil, ErrUserNotFound
	}
	copy := *u
	return &copy, nil
}

type resetServiceRepo struct {
	AccountRecoveryCodeRepository
	called int
	target int64
	epoch  string
	err    error
	commit func(int64, string)
}

func (r *resetServiceRepo) ResetAccountSecurity(_ context.Context, id int64, epoch string) error {
	r.called++
	r.target = id
	r.epoch = epoch
	if r.err != nil {
		return r.err
	}
	if r.commit != nil {
		r.commit(id, epoch)
	}
	return nil
}

type resetServiceCache struct {
	*epochRefreshCache
	cleaned     []int64
	afterCommit func()
}

func (c *resetServiceCache) DeleteUserRefreshTokens(ctx context.Context, id int64) error {
	if c.afterCommit != nil {
		c.afterCommit()
	}
	c.cleaned = append(c.cleaned, id)
	return c.epochRefreshCache.DeleteUserRefreshTokens(ctx, id)
}
func resetServiceFixture(t *testing.T) (*AuthService, *resetServiceRepo, *resetServiceCache, *epochSettingsRepo, *User, *User) {
	t.Helper()
	auth, settings, cache, operator := epochTestService()
	auth.cfg.AccountCenter.Enabled = true
	operator.Role = RoleAdmin
	target := &User{ID: 77, Email: "target@example.test", PasswordHash: "preserved", Status: StatusActive, Balance: 123, Role: RoleUser}
	auth.userRepo = &resetServiceUserRepo{users: map[int64]*User{operator.ID: operator, target.ID: target}}
	repo := &resetServiceRepo{}
	repo.commit = func(id int64, epoch string) {
		require.Equal(t, target.ID, id)
		require.NoError(t, settings.Set(context.Background(), sessionEpochSettingKey(id), epoch))
		target.TotpEnabled = false
		target.TotpSecretEncrypted = nil
		target.TotpEnabledAt = nil
	}
	cc := &resetServiceCache{epochRefreshCache: cache}
	auth.refreshTokenCache = cc
	auth.recoveryCodes = repo
	return auth, repo, cc, settings, operator, target
}
func resetServiceToken(t *testing.T, auth *AuthService, user *User, methods []string, when int64) string {
	t.Helper()
	token, err := auth.GenerateToken(WithNativeAuthenticationProof(context.Background(), NativeAuthenticationProof{Methods: methods, AuthTime: when}), user)
	require.NoError(t, err)
	return token
}
func TestAccountSecurityResetServiceSignedFactors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mfa     bool
		methods []string
		age     time.Duration
		allow   bool
	}{
		{"password", false, []string{"pwd"}, 0, true},
		{"totp", true, []string{"pwd", "otp"}, 0, true},
		{"passkey", true, []string{"passkey"}, 0, true},
		{"mfa_password_only", true, []string{"pwd"}, 0, false},
		{"backup_recovery_factor", true, []string{"pwd", "recovery"}, 0, true},
		{"recovery_without_password", true, []string{"recovery"}, 0, false},
		{"email_code", true, []string{"pwd", "email"}, 0, false},
		{"otp_without_password", true, []string{"otp"}, 0, false},
		{"no_factors", false, nil, 0, false},
		{"stale", true, []string{"pwd", "otp"}, 6 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, repo, cache, _, operator, target := resetServiceFixture(t)
			operator.TotpEnabled = tc.mfa
			token := resetServiceToken(t, auth, operator, tc.methods, time.Now().Add(-tc.age).Unix())
			err := auth.ResetAccountSecurity(context.Background(), operator.ID, target.ID, token, true, "Identity verified manually")
			if tc.allow {
				require.NoError(t, err)
				require.Equal(t, 1, repo.called)
				require.Equal(t, []int64{target.ID}, cache.cleaned)
			} else {
				require.Error(t, err)
				require.Zero(t, repo.called)
				require.Empty(t, cache.cleaned)
			}
		})
	}
}
func TestAccountSecurityResetServiceAuthorizationAndConfirmation(t *testing.T) {
	for _, mode := range []string{"disabled", "inactive", "non_admin", "deleted", "wrong_token_user", "forged_token", "changed_generation", "revoked_epoch", "context_proof_only", "unconfirmed", "empty_note", "whitespace_note", "long_note", "invalid_utf8", "invalid_target", "missing_capability", "missing_epoch_store"} {
		t.Run(mode, func(t *testing.T) {
			auth, repo, cache, settings, operator, target := resetServiceFixture(t)
			token := resetServiceToken(t, auth, operator, []string{"pwd"}, time.Now().Unix())
			confirmed, note := true, "Manually checked identity"
			ctx := context.Background()
			switch mode {
			case "disabled":
				auth.cfg.AccountCenter.Enabled = false
			case "inactive":
				operator.Status = "disabled"
			case "non_admin":
				operator.Role = RoleUser
			case "deleted":
				now := time.Now()
				operator.DeletedAt = &now
			case "wrong_token_user":
				token = resetServiceToken(t, auth, target, []string{"pwd"}, time.Now().Unix())
			case "forged_token":
				token += "x"
			case "changed_generation":
				operator.PasswordHash = "new"
			case "revoked_epoch":
				settings.values[sessionEpochSettingKey(operator.ID)] = strings.Repeat("ab", 16)
			case "context_proof_only":
				ctx = WithNativeAuthenticationProof(ctx, NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: time.Now().Unix()})
				token = ""
			case "unconfirmed":
				confirmed = false
			case "empty_note":
				note = ""
			case "whitespace_note":
				note = " \t\n "
			case "long_note":
				note = strings.Repeat("x", AccountSecurityResetNoteMaxBytes+1)
			case "invalid_utf8":
				note = string([]byte{0xff})
			case "invalid_target":
				target.ID = 0
			case "missing_capability":
				auth.recoveryCodes = struct{ AccountRecoveryCodeRepository }{}
			case "missing_epoch_store":
				auth.settingService = nil
			}
			require.Error(t, auth.ResetAccountSecurity(ctx, operator.ID, target.ID, token, confirmed, note))
			require.Zero(t, repo.called)
			require.Empty(t, cache.cleaned)
		})
	}
}
func TestAccountSecurityResetServiceDurableFenceBeforeCleanup(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cleanup_unknown"}[cleanupFailure], func(t *testing.T) {
			auth, repo, cache, settings, operator, target := resetServiceFixture(t)
			target.TotpEnabled = true
			secret := "synthetic-encrypted"
			target.TotpSecretEncrypted = &secret
			before := *target
			oldAccess := resetServiceToken(t, auth, target, []string{"pwd", "otp"}, time.Now().Unix())
			oldPair, err := auth.GenerateTokenPair(WithNativeAuthenticationProof(context.Background(), NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: time.Now().Unix()}), target, "")
			require.NoError(t, err)
			operatorToken := resetServiceToken(t, auth, operator, []string{"pwd"}, time.Now().Unix())
			cache.afterCommit = func() {
				require.NotEmpty(t, settings.values[sessionEpochSettingKey(target.ID)])
				require.False(t, target.TotpEnabled)
			}
			if cleanupFailure {
				cache.cleanupErr = errors.New("redis private material")
			}
			err = auth.ResetAccountSecurity(context.Background(), operator.ID, target.ID, operatorToken, true, strings.Repeat("x", AccountSecurityResetNoteMaxBytes))
			if cleanupFailure {
				require.ErrorIs(t, err, ErrAccountSecurityResetUnknown)
				require.NotContains(t, err.Error(), "private material")
			} else {
				require.NoError(t, err)
			}
			decoded, err := hex.DecodeString(repo.epoch)
			require.NoError(t, err)
			require.Len(t, decoded, 16)
			require.Equal(t, repo.epoch, settings.values[sessionEpochSettingKey(target.ID)])
			require.Equal(t, before.Email, target.Email)
			require.Equal(t, before.PasswordHash, target.PasswordHash)
			require.Equal(t, before.ID, target.ID)
			require.Equal(t, before.Balance, target.Balance)
			require.Equal(t, before.Status, target.Status)
			_, err = auth.ValidateTokenContext(context.Background(), oldAccess)
			require.ErrorIs(t, err, ErrTokenRevoked)
			_, err = auth.RefreshTokenPair(context.Background(), oldPair.RefreshToken)
			require.Error(t, err)
		})
	}
}
func TestAccountSecurityResetServiceFailureNeverCleansRedis(t *testing.T) {
	for _, storageErr := range []error{errors.New("driver private material"), ErrUserNotFound, ErrAccountSecurityResetUnknown} {
		auth, repo, cache, settings, operator, target := resetServiceFixture(t)
		token := resetServiceToken(t, auth, operator, []string{"pwd"}, time.Now().Unix())
		repo.err = storageErr
		err := auth.ResetAccountSecurity(context.Background(), operator.ID, target.ID, token, true, "Verified")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private material")
		require.Empty(t, cache.cleaned)
		require.Empty(t, settings.values)
	}
}
func TestAccountSecurityResetServiceFreshEpochEachTime(t *testing.T) {
	auth, repo, _, _, operator, target := resetServiceFixture(t)
	token := resetServiceToken(t, auth, operator, []string{"pwd"}, time.Now().Unix())
	require.NoError(t, auth.ResetAccountSecurity(context.Background(), operator.ID, target.ID, token, true, "Verified"))
	first := repo.epoch
	require.NoError(t, auth.ResetAccountSecurity(context.Background(), operator.ID, target.ID, token, true, "Verified again"))
	require.NotEqual(t, first, repo.epoch)
}
