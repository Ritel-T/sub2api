//go:build unit

package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type credentialChangeCache struct {
	failUserIndex, failFamilyIndex bool
	*emailBindRefreshTokenCacheStub
	beforeStore          func()
	afterStore           func()
	cleanupErr, storeErr error
	minted               []*service.RefreshTokenData
}

func (c *credentialChangeCache) StoreRefreshToken(ctx context.Context, hash string, data *service.RefreshTokenData, ttl time.Duration) error {
	if c.beforeStore != nil {
		c.beforeStore()
	}
	copy := *data
	c.minted = append(c.minted, &copy)
	if c.storeErr != nil {
		return c.storeErr
	}
	if err := c.emailBindRefreshTokenCacheStub.StoreRefreshToken(ctx, hash, data, ttl); err != nil {
		return err
	}
	if c.afterStore != nil {
		c.afterStore()
	}
	return nil
}
func (c *credentialChangeCache) DeleteUserRefreshTokens(ctx context.Context, id int64) error {
	if c.cleanupErr != nil {
		return c.cleanupErr
	}
	return c.emailBindRefreshTokenCacheStub.DeleteUserRefreshTokens(ctx, id)
}

func (c *credentialChangeCache) AddToUserTokenSet(ctx context.Context, id int64, hash string, ttl time.Duration) error {
	if c.failUserIndex {
		return errors.New("ambiguous user index")
	}
	return c.emailBindRefreshTokenCacheStub.AddToUserTokenSet(ctx, id, hash, ttl)
}
func (c *credentialChangeCache) AddToFamilyTokenSet(ctx context.Context, id, hash string, ttl time.Duration) error {
	if c.failFamilyIndex {
		return errors.New("ambiguous family index")
	}
	return c.emailBindRefreshTokenCacheStub.AddToFamilyTokenSet(ctx, id, hash, ttl)
}

type credentialChangeUsers struct {
	service.UserRepository
	afterRead func(context.Context)
}

func (r *credentialChangeUsers) GetByID(ctx context.Context, id int64) (*service.User, error) {
	u, err := r.UserRepository.GetByID(ctx, id)
	if err == nil && r.afterRead != nil {
		r.afterRead(ctx)
	}
	return u, err
}
func (r *credentialChangeUsers) UpdateEmailWithAliasGuard(ctx context.Context, id int64, email, hash string) error {
	return r.UserRepository.(interface {
		UpdateEmailWithAliasGuard(context.Context, int64, string, string) error
	}).UpdateEmailWithAliasGuard(ctx, id, email, hash)
}

// Model actual code consumption so password-failure ordering cannot pass
// merely because the older email test stub leaves successful codes reusable.
type credentialChangeEmailCache struct {
	*emailBindCacheStub
	reads, attempts, consumed int
}

func (c *credentialChangeEmailCache) GetVerificationCode(ctx context.Context, email string) (*service.VerificationCodeData, error) {
	c.reads++
	return c.emailBindCacheStub.GetVerificationCode(ctx, email)
}
func (c *credentialChangeEmailCache) IncrVerificationCodeAttempts(ctx context.Context, email string) (int, error) {
	c.attempts++
	return c.emailBindCacheStub.IncrVerificationCodeAttempts(ctx, email)
}
func (c *credentialChangeEmailCache) DeleteVerificationCode(context.Context, string) error {
	c.consumed++
	c.data = nil
	return nil
}

type credentialChangeFixture struct {
	emailCache *credentialChangeEmailCache
	auth       *service.AuthService
	client     *dbent.Client
	users      *credentialChangeUsers
	settings   service.SettingRepository
	cache      *credentialChangeCache
	ctx        context.Context
	user       *service.User
}

func newCredentialChangeFixture(t *testing.T, mfa bool) *credentialChangeFixture {
	t.Helper()
	_, repo, client := newAuthServiceForEmailBind(t, nil, nil, nil)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "synthetic-credential-key", ExpireHour: 1, RefreshTokenExpireDays: 7}, AccountCenter: config.AccountCenterConfig{Enabled: true}}
	settings := repository.NewSettingRepository(client)
	users := &credentialChangeUsers{UserRepository: repo}
	cache := &credentialChangeCache{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub()}
	emailCache := &credentialChangeEmailCache{emailBindCacheStub: &emailBindCacheStub{data: &service.VerificationCodeData{Code: "123456", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}}}
	email := service.NewEmailService(settings, emailCache)
	auth := service.NewAuthService(client, users, nil, cache, cfg, service.NewSettingService(settings, cfg), email, nil, nil, nil, nil, nil, nil)
	ctx := service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: "127.0.0.1", UserAgent: "native-credential-test"})
	hash, err := auth.HashPassword("old-password")
	require.NoError(t, err)
	u := &service.User{Email: "original@example.test", Username: "native user", PasswordHash: hash, Role: service.RoleUser, Status: service.StatusActive, Balance: 17.25, Concurrency: 5}
	require.NoError(t, repo.Create(ctx, u))
	if mfa {
		_, err := client.User.UpdateOneID(u.ID).SetTotpEnabled(true).SetTotpSecretEncrypted("preserved-factor").SetTotpEnabledAt(time.Now().Add(-time.Hour)).Save(ctx)
		require.NoError(t, err)
	}
	u, err = repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	return &credentialChangeFixture{emailCache: emailCache, auth: auth, client: client, users: users, settings: settings, cache: cache, ctx: ctx, user: u}
}
func (f *credentialChangeFixture) pair(t *testing.T, methods []string, age time.Duration) *service.TokenPair {
	t.Helper()
	pair, err := f.auth.GenerateTokenPair(service.WithNativeAuthenticationProof(f.ctx, service.NativeAuthenticationProof{Methods: methods, AuthTime: time.Now().Add(-age).Unix()}), f.user, "")
	require.NoError(t, err)
	return pair
}
func credentialHash(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func TestAccountSecurityCredentialChangePreservesProofAndBusiness(t *testing.T) {
	for _, kind := range []string{"password", "email"} {
		t.Run(kind, func(t *testing.T) {
			f := newCredentialChangeFixture(t, true)
			before := f.user
			// Nonempty captured epoch must survive mint unchanged.
			require.NoError(t, f.settings.Set(f.ctx, service.AuthSessionEpochSettingPrefix+strconv.FormatInt(f.user.ID, 10), "00112233445566778899aabbccddeeff"))
			key, err := f.client.APIKey.Create().SetUserID(before.ID).SetKey("sk-credential-preserved").SetName("preserved").SetStatus(service.StatusActive).Save(f.ctx)
			require.NoError(t, err)
			old := f.pair(t, []string{"pwd", "otp"}, 2*time.Minute)
			other := f.pair(t, []string{"pwd", "otp"}, time.Minute)
			oldClaims, err := f.auth.ValidateTokenContext(f.ctx, old.AccessToken)
			require.NoError(t, err)
			var result *service.AccountCredentialChange
			if kind == "password" {
				result, err = f.auth.ChangeAccountSecurityPassword(f.ctx, before.ID, old.AccessToken, "old-password", "new-password")
			} else {
				result, err = f.auth.ChangeAccountSecurityEmail(f.ctx, before.ID, old.AccessToken, "replacement@example.test", "123456", "old-password")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotEmpty(t, result.AccessToken)
			require.NotEmpty(t, result.RefreshToken)
			require.Greater(t, result.ExpiresIn, 0)
			require.Greater(t, result.RefreshExpiresAt, time.Now().Unix())
			claims, err := f.auth.RequireRecentAccountAuthentication(f.ctx, result.AccessToken, before.ID, true)
			require.NoError(t, err)
			require.Equal(t, oldClaims.AuthenticationMethods, claims.AuthenticationMethods)
			require.Equal(t, oldClaims.AuthenticationTime, claims.AuthenticationTime)
			require.Equal(t, oldClaims.SessionEpoch, claims.SessionEpoch)
			require.NotEmpty(t, claims.SessionEpoch)
			require.NotEqual(t, oldClaims.SessionID, claims.SessionID)
			require.NotEqual(t, oldClaims.AccountGeneration, claims.AccountGeneration)
			require.Equal(t, service.SessionBindingFromContext(f.ctx).Hash(), claims.BindingHash)
			stored, err := f.cache.GetRefreshToken(f.ctx, credentialHash(result.RefreshToken))
			require.NoError(t, err)
			require.Equal(t, claims.SessionID, stored.FamilyID)
			require.Equal(t, claims.AuthenticationTime, stored.AuthenticationTime)
			require.Equal(t, claims.AuthenticationMethods, stored.AuthenticationMethods)
			require.Equal(t, claims.SessionEpoch, stored.SessionEpoch)
			require.Equal(t, claims.AccountGeneration, stored.AccountGeneration)
			require.Equal(t, claims.BindingHash, stored.BindingHash)
			require.Equal(t, stored.ExpiresAt.Unix(), result.RefreshExpiresAt)
			for _, pair := range []*service.TokenPair{old, other} {
				_, err = f.auth.RefreshToken(f.ctx, pair.AccessToken)
				require.ErrorIs(t, err, service.ErrTokenRevoked)
				_, err = f.auth.RefreshTokenPair(f.ctx, pair.RefreshToken)
				require.Error(t, err)
				_, err = f.auth.RequireRecentAccountAuthentication(f.ctx, pair.AccessToken, before.ID, true)
				require.ErrorIs(t, err, service.ErrTokenRevoked)
			}
			current, err := f.users.GetByID(f.ctx, before.ID)
			require.NoError(t, err)
			require.Equal(t, before.Balance, current.Balance)
			require.Equal(t, before.Concurrency, current.Concurrency)
			require.Equal(t, before.TotpEnabled, current.TotpEnabled)
			require.Equal(t, before.TotpSecretEncrypted, current.TotpSecretEncrypted)
			require.Equal(t, before.TotpEnabledAt, current.TotpEnabledAt)
			keyAfter, err := f.client.APIKey.Get(f.ctx, key.ID)
			require.NoError(t, err)
			require.Equal(t, key.Key, keyAfter.Key)
			require.Equal(t, key.Status, keyAfter.Status)
			if kind == "password" {
				require.True(t, f.auth.CheckPassword("new-password", current.PasswordHash))
				require.Equal(t, before.Email, current.Email)
			} else {
				require.Equal(t, "replacement@example.test", current.Email)
				require.Equal(t, before.PasswordHash, current.PasswordHash)
				identity, err := f.client.AuthIdentity.Query().Only(f.ctx)
				require.NoError(t, err)
				require.Equal(t, current.ID, identity.UserID)
				require.Equal(t, current.Email, identity.ProviderSubject)
			}
			refreshed, err := f.auth.RefreshTokenPair(f.ctx, result.RefreshToken)
			require.NoError(t, err)
			refreshClaims, err := f.auth.ValidateTokenContext(f.ctx, refreshed.AccessToken)
			require.NoError(t, err)
			require.Equal(t, oldClaims.AuthenticationTime, refreshClaims.AuthenticationTime)
		})
	}
}

func TestAccountSecurityCredentialRejectsUntrustedOrInsufficientProof(t *testing.T) {
	for _, tc := range []struct {
		name    string
		methods []string
		age     time.Duration
	}{
		{"password_without_mfa", []string{"pwd"}, 0},
		{"email_is_not_mfa", []string{"pwd", "email"}, 0},
		{"stale", []string{"pwd", "otp"}, 31 * time.Minute},
		{"no_native_factors", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCredentialChangeFixture(t, true)
			pair := f.pair(t, tc.methods, tc.age)
			ctx := service.WithNativeAuthenticationProof(f.ctx, service.NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: time.Now().Unix(), Generation: "caller-asserted"})
			result, err := f.auth.ChangeAccountSecurityPassword(ctx, f.user.ID, pair.AccessToken, "old-password", "new-password")
			require.Error(t, err)
			require.Nil(t, result)
			current, err := f.users.GetByID(f.ctx, f.user.ID)
			require.NoError(t, err)
			require.Equal(t, f.user.PasswordHash, current.PasswordHash)
			require.Len(t, f.cache.minted, 1)
		})
	}
	f := newCredentialChangeFixture(t, false)
	old := f.pair(t, []string{"pwd"}, 0)
	_, err := f.auth.ChangeAccountSecurityPassword(f.ctx, f.user.ID+1, old.AccessToken, "old-password", "new-password")
	require.Error(t, err)
	mismatch := service.WithSessionBinding(f.ctx, &service.SessionBinding{IP: "127.0.0.2", UserAgent: "native-credential-test"})
	_, err = f.auth.ChangeAccountSecurityPassword(mismatch, f.user.ID, old.AccessToken, "old-password", "new-password")
	require.ErrorIs(t, err, service.ErrSessionBindingMismatch)
	_, err = f.auth.ChangeAccountSecurityPassword(f.ctx, f.user.ID, old.AccessToken, "wrong", "new-password")
	require.ErrorIs(t, err, service.ErrPasswordIncorrect)
	result, err := f.auth.ChangeAccountSecurityPassword(f.ctx, f.user.ID, old.AccessToken, "old-password", "new-password")
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestAccountSecurityCredentialCASRejectsConcurrentMutation(t *testing.T) {
	f := newCredentialChangeFixture(t, false)
	old := f.pair(t, []string{"pwd"}, 0)
	reads := 0
	f.users.afterRead = func(ctx context.Context) {
		if dbent.TxFromContext(ctx) != nil {
			return
		}
		reads++
		if reads == 2 {
			_, err := f.client.User.UpdateOneID(f.user.ID).SetPasswordHash("concurrent-password-hash").Save(f.ctx)
			require.NoError(t, err)
		}
	}
	result, err := f.auth.ChangeAccountSecurityPassword(f.ctx, f.user.ID, old.AccessToken, "old-password", "new-password")
	require.ErrorIs(t, err, service.ErrTokenRevoked)
	require.Nil(t, result)
	require.Len(t, f.cache.minted, 1)
	current, err := f.users.GetByID(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Equal(t, "concurrent-password-hash", current.PasswordHash)
}

func TestAccountSecurityCredentialDoesNotResurrectConcurrentRevocation(t *testing.T) {
	for _, mode := range []string{"epoch_before_mint", "epoch_during_mint", "admin_reset_during_mint", "credential_during_mint", "family_during_mint", "cleanup_failure", "mint_failure", "user_index_failure", "family_index_failure", "commit_ack_lost"} {
		t.Run(mode, func(t *testing.T) {
			f := newCredentialChangeFixture(t, true)
			old := f.pair(t, []string{"pwd", "otp"}, time.Minute)
			original, err := f.auth.ValidateTokenContext(f.ctx, old.AccessToken)
			require.NoError(t, err)
			concurrent := func() {
				switch mode {
				case "epoch_before_mint", "epoch_during_mint", "admin_reset_during_mint":
					require.NoError(t, f.settings.Set(f.ctx, service.AuthSessionEpochSettingPrefix+strconv.FormatInt(f.user.ID, 10), "ffeeddccbbaa99887766554433221100"))
					if mode == "admin_reset_during_mint" {
						_, err := f.client.User.UpdateOneID(f.user.ID).SetTotpEnabled(false).ClearTotpSecretEncrypted().Save(f.ctx)
						require.NoError(t, err)
					}
				case "credential_during_mint":
					_, err := f.client.User.UpdateOneID(f.user.ID).SetPasswordHash("unrelated-concurrent-password").Save(f.ctx)
					require.NoError(t, err)
				case "family_during_mint":
					require.NoError(t, f.settings.Set(f.ctx, service.AuthSessionEpochSettingPrefix+"family:"+strconv.FormatInt(f.user.ID, 10)+":"+original.SessionID, "revoked"))
				}
			}
			if mode == "epoch_before_mint" {
				f.users.afterRead = func(ctx context.Context) {
					if dbent.TxFromContext(ctx) == nil {
						u, err := f.users.UserRepository.GetByID(ctx, f.user.ID)
						require.NoError(t, err)
						if u.PasswordHash != f.user.PasswordHash {
							f.users.afterRead = nil
							concurrent()
						}
					}
				}
			} else if mode == "commit_ack_lost" {
				f.users.afterRead = func(ctx context.Context) {
					if tx := dbent.TxFromContext(ctx); tx != nil {
						tx.OnCommit(func(next dbent.Committer) dbent.Committer {
							return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
								if err := next.Commit(ctx, tx); err != nil {
									return err
								}
								return errors.New("commit acknowledgement lost")
							})
						})
					}
				}
			} else if mode == "user_index_failure" {
				f.cache.failUserIndex = true
			} else if mode == "family_index_failure" {
				f.cache.failFamilyIndex = true
			} else if mode == "cleanup_failure" {
				f.cache.cleanupErr = errors.New("ambiguous cleanup")
			} else if mode == "mint_failure" {
				f.cache.storeErr = errors.New("ambiguous store")
			} else {
				f.cache.afterStore = concurrent
			}
			result, err := f.auth.ChangeAccountSecurityPassword(f.ctx, f.user.ID, old.AccessToken, "old-password", "new-password")
			require.ErrorIs(t, err, service.ErrAccountCredentialChangeUnknown)
			require.Nil(t, result)
			current, err := f.users.GetByID(f.ctx, f.user.ID)
			require.NoError(t, err)
			require.NotEqual(t, f.user.PasswordHash, current.PasswordHash)
			if len(f.cache.minted) > 1 {
				issued := f.cache.minted[1]
				require.Equal(t, original.SessionEpoch, issued.SessionEpoch)
				require.Equal(t, original.AuthenticationTime, issued.AuthenticationTime)
				require.NotEqual(t, original.SessionID, issued.FamilyID)
			}
			// The old credential remains fenced even when cleanup/mint is uncertain.
			_, err = f.auth.RequireRecentAccountAuthentication(f.ctx, old.AccessToken, f.user.ID, true)
			require.Error(t, err)
		})
	}
}

func TestAccountSecurityCredentialEmailFirstBindPreservesBusinessDefaults(t *testing.T) {
	f := newCredentialChangeFixture(t, false)
	_, err := f.client.User.UpdateOneID(f.user.ID).SetEmail("synthetic" + service.OIDCConnectSyntheticEmailDomain).Save(f.ctx)
	require.NoError(t, err)
	f.user, err = f.users.GetByID(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.NoError(t, f.settings.Set(f.ctx, service.SettingKeyAuthSourceDefaultEmailBalance, "100"))
	require.NoError(t, f.settings.Set(f.ctx, service.SettingKeyAuthSourceDefaultEmailConcurrency, "20"))
	old := f.pair(t, []string{"passkey"}, time.Minute)
	_, err = f.auth.ChangeAccountSecurityEmail(f.ctx, f.user.ID, old.AccessToken, "bound@example.test", "wrong-code", "new-password")
	require.ErrorIs(t, err, service.ErrInvalidVerifyCode)
	unchanged, err := f.users.GetByID(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Equal(t, f.user.Email, unchanged.Email)
	require.Equal(t, f.user.PasswordHash, unchanged.PasswordHash)
	result, err := f.auth.ChangeAccountSecurityEmail(f.ctx, f.user.ID, old.AccessToken, "bound@example.test", "123456", "new-password")
	require.NoError(t, err)
	require.Equal(t, f.user.Balance, result.User.Balance)
	require.Equal(t, f.user.Concurrency, result.User.Concurrency)
	require.True(t, f.auth.CheckPassword("new-password", result.User.PasswordHash))
	claims, err := f.auth.RequireRecentAccountAuthentication(f.ctx, result.AccessToken, f.user.ID, false)
	require.NoError(t, err)
	require.Equal(t, []string{"passkey"}, claims.AuthenticationMethods)
}

func TestAccountSecurityCredentialPasswordUnicodeLimits(t *testing.T) {
	for _, operation := range []string{"password", "first_email_bind"} {
		for _, tc := range []struct{ name, password, reason string }{
			{"five_runes", strings.Repeat("界", 5), "PASSWORD_TOO_SHORT"},
			{"six_runes", strings.Repeat("界", 6), ""},
			{"72_bytes", strings.Repeat("界", 24), ""},
			{"73_bytes", strings.Repeat("界", 24) + "a", "PASSWORD_TOO_LONG"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				f := newCredentialChangeFixture(t, false)
				if operation == "first_email_bind" {
					_, err := f.client.User.UpdateOneID(f.user.ID).SetEmail("synthetic" + service.OIDCConnectSyntheticEmailDomain).Save(f.ctx)
					require.NoError(t, err)
					f.user, err = f.users.GetByID(f.ctx, f.user.ID)
					require.NoError(t, err)
				}
				old := f.pair(t, []string{"pwd"}, time.Minute)
				var result *service.AccountCredentialChange
				var err error
				if operation == "password" {
					result, err = f.auth.ChangeAccountSecurityPassword(f.ctx, f.user.ID, old.AccessToken, "old-password", tc.password)
				} else {
					result, err = f.auth.ChangeAccountSecurityEmail(f.ctx, f.user.ID, old.AccessToken, "bound@example.test", "123456", tc.password)
				}
				if tc.reason != "" {
					require.Error(t, err)
					require.Equal(t, tc.reason, infraerrors.Reason(err))
					require.Equal(t, 400, infraerrors.Code(err))
					require.Nil(t, result)
					current, readErr := f.users.GetByID(f.ctx, f.user.ID)
					require.NoError(t, readErr)
					require.Equal(t, f.user.Email, current.Email)
					require.Equal(t, f.user.PasswordHash, current.PasswordHash)
					require.Zero(t, f.emailCache.reads)
					require.Zero(t, f.emailCache.attempts)
					require.Zero(t, f.emailCache.consumed)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					require.True(t, f.auth.CheckPassword(tc.password, result.User.PasswordHash))
				}
			})
		}
	}
}

func TestAccountSecurityCredentialEmailWrongPasswordDoesNotConsumeCode(t *testing.T) {
	f := newCredentialChangeFixture(t, false)
	old := f.pair(t, []string{"pwd"}, time.Minute)
	result, err := f.auth.ChangeAccountSecurityEmail(f.ctx, f.user.ID, old.AccessToken, "changed@example.test", "123456", "wrong-password")
	require.ErrorIs(t, err, service.ErrPasswordIncorrect)
	require.Equal(t, "PASSWORD_INCORRECT", infraerrors.Reason(err))
	require.Nil(t, result)
	require.Zero(t, f.emailCache.reads)
	require.Zero(t, f.emailCache.attempts)
	require.Zero(t, f.emailCache.consumed)
	require.NotNil(t, f.emailCache.data)
	// Same native session and exact code can complete after correcting password.
	result, err = f.auth.ChangeAccountSecurityEmail(f.ctx, f.user.ID, old.AccessToken, "changed@example.test", "123456", "old-password")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "changed@example.test", result.User.Email)
	require.Equal(t, 1, f.emailCache.reads)
	require.Equal(t, 1, f.emailCache.attempts)
	require.Equal(t, 1, f.emailCache.consumed)
	require.Nil(t, f.emailCache.data)
}
