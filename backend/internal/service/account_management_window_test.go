//go:build unit

package service

import (
	"context"
	"net/url"
	"testing"
	"testing/synctest"
	"time"

	"github.com/pquerna/otp"
	"github.com/stretchr/testify/require"
)

type accountWindowCache struct {
	TotpCache
	setup         *TotpSetupSession
	setupTTL      time.Duration
	grantTTL      time.Duration
	grantDeadline time.Time
	grantSession  string
	grantWrites   int
}

func (c *accountWindowCache) SetSetupSession(_ context.Context, _ int64, session *TotpSetupSession, ttl time.Duration) error {
	c.setup, c.setupTTL = session, ttl
	return nil
}
func (c *accountWindowCache) SetStepUpGrant(_ context.Context, _ int64, session string, ttl time.Duration) error {
	c.grantTTL, c.grantDeadline, c.grantSession = ttl, time.Now().Add(ttl), session
	c.grantWrites++
	return nil
}

func TestAccountManagementWindowAndStepUpDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		auth, _, _, user := epochTestService()
		auth.cfg.AccountCenter.Enabled = true
		user.TotpEnabled = true
		authenticatedAt := time.Now()
		pair, err := auth.GenerateTokenPair(WithNativeAuthenticationProof(ctx, NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: authenticatedAt.Unix()}), user, "")
		require.NoError(t, err)
		claims, err := auth.ValidateTokenContext(ctx, pair.AccessToken)
		require.NoError(t, err)
		cache := &accountWindowCache{}
		totpService := &TotpService{cache: cache, userRepo: auth.userRepo}
		grant := func(token string, want time.Duration) {
			t.Helper()
			ttl, err := totpService.GrantAccountStepUp(ctx, auth, user.ID, claims.SessionID, token)
			require.NoError(t, err)
			require.Equal(t, want, ttl)
			require.Equal(t, ttl, cache.grantTTL)
			require.Equal(t, claims.SessionID, cache.grantSession)
			require.Equal(t, authenticatedAt.Add(30*time.Minute), cache.grantDeadline)
		}
		grant(pair.AccessToken, 30*time.Minute)
		// Refresh and repeat exchange must preserve the original deadline.
		time.Sleep(6 * time.Minute)
		rotated, err := auth.RefreshTokenPair(ctx, pair.RefreshToken)
		require.NoError(t, err)
		refreshed, err := auth.RequireRecentAccountAuthentication(ctx, rotated.AccessToken, user.ID, true)
		require.NoError(t, err)
		require.Equal(t, claims.AuthenticationTime, refreshed.AuthenticationTime)
		grant(rotated.AccessToken, 24*time.Minute)
		time.Sleep(23*time.Minute + 59*time.Second)
		_, err = auth.RequireRecentAccountAuthentication(ctx, rotated.AccessToken, user.ID, true)
		require.NoError(t, err)
		grant(rotated.AccessToken, time.Second)
		time.Sleep(time.Second)
		// Recent-proof comparison retains its inclusive second boundary;
		// a grant needs positive whole seconds and cannot outlive auth_time+30m.
		_, err = auth.RequireRecentAccountAuthentication(ctx, rotated.AccessToken, user.ID, true)
		require.NoError(t, err)
		writes := cache.grantWrites
		ttl, err := totpService.GrantAccountStepUp(ctx, auth, user.ID, claims.SessionID, rotated.AccessToken)
		require.Error(t, err)
		require.Zero(t, ttl)
		require.Equal(t, writes, cache.grantWrites)
		time.Sleep(time.Second)
		_, err = auth.RequireRecentAccountAuthentication(ctx, rotated.AccessToken, user.ID, true)
		require.Error(t, err)
		_, err = totpService.GrantAccountStepUp(ctx, auth, user.ID, claims.SessionID, rotated.AccessToken)
		require.Error(t, err)
		require.Equal(t, writes, cache.grantWrites)
	})
}

func TestAccountManagementStepUpFutureClockSkewIsCapped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		auth, _, _, user := epochTestService()
		auth.cfg.AccountCenter.Enabled = true
		user.TotpEnabled = true
		cache := &accountWindowCache{}
		svc := &TotpService{cache: cache, userRepo: auth.userRepo}
		for _, skew := range []time.Duration{5 * time.Second, 6 * time.Second} {
			token, err := auth.GenerateToken(WithNativeAuthenticationProof(ctx, NativeAuthenticationProof{Methods: []string{"pwd", "otp"}, AuthTime: time.Now().Add(skew).Unix()}), user)
			require.NoError(t, err)
			claims, err := auth.ValidateTokenContext(ctx, token)
			require.NoError(t, err)
			ttl, err := svc.GrantAccountStepUp(ctx, auth, user.ID, claims.SessionID, token)
			if skew == 5*time.Second {
				require.NoError(t, err)
				require.Equal(t, 30*time.Minute, ttl)
			} else {
				require.Error(t, err)
				require.Zero(t, ttl)
			}
		}
	})
}

func TestAccountManagementTotpSetupIssuerAndLifetime(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy_setup", true: "account_setup"}[native], func(t *testing.T) {
			ctx := context.Background()
			auth, settings, _, user := epochTestService()
			auth.cfg.AccountCenter.Enabled = true
			settings.values[SettingKeyTotpEnabled] = "true"
			require.NoError(t, user.SetPassword("synthetic-setup-password"))
			cache := &accountWindowCache{}
			svc := &TotpService{cache: cache, userRepo: auth.userRepo, settingService: auth.settingService}
			var result *TotpSetupResponse
			var err error
			if native {
				token := resetServiceToken(t, auth, user, []string{"pwd"}, time.Now().Unix())
				result, err = svc.BeginAccountSetup(ctx, auth, user.ID, token)
			} else {
				result, err = svc.InitiateSetup(ctx, user.ID, "", "synthetic-setup-password")
			}
			require.NoError(t, err)
			require.Equal(t, 1800, result.Countdown)
			require.Equal(t, 30*time.Minute, cache.setupTTL)
			require.Equal(t, result.SetupToken, cache.setup.SetupToken)
			require.Equal(t, result.Secret, cache.setup.Secret)
			uri, err := url.Parse(result.QRCodeURL)
			require.NoError(t, err)
			require.Equal(t, "otpauth", uri.Scheme)
			require.Equal(t, "totp", uri.Host)
			require.Equal(t, "/RitelSI:"+user.Email, uri.Path)
			require.Equal(t, "RitelSI", uri.Query().Get("issuer"))
			key, err := otp.NewKeyFromURL(result.QRCodeURL)
			require.NoError(t, err)
			require.EqualValues(t, 30, key.Period())
			require.Equal(t, user.Email, key.AccountName())
		})
	}
	require.Equal(t, 5*time.Minute, totpLoginTTL)
	require.Equal(t, 15*time.Minute, totpAttemptsTTL)
	require.Equal(t, 5, maxTotpAttempts)
}
