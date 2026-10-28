//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func accountCenterRefreshClaims(t *testing.T, s *AuthService, token string) *JWTClaims {
	t.Helper()
	claims := &JWTClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return []byte(s.cfg.JWT.Secret), nil })
	require.NoError(t, err)
	return claims
}

func TestAccountCenterRefresh_AuthoritativeExpiryAndGenerationSurviveRotation(t *testing.T) {
	ctx := context.Background()
	s, settings, cache, user := epochTestService()
	s.cfg.AccountCenter.Enabled = true
	s.cfg.JWT.RefreshTokenExpireDays = 9 // Deliberately differs from a client-assumed 30 days.
	settings.values[sessionEpochSettingKey(user.ID)] = strings.Repeat("ab", 16)
	// Production family IDs are 32 hex characters and are validated by the
	// durable family fence on refresh. Keep the rotation fixture within that contract.
	originalFamilyID := strings.Repeat("cd", 16)
	pair, err := s.GenerateTokenPair(ctx, user, originalFamilyID)
	require.NoError(t, err)
	original, err := cache.GetRefreshToken(ctx, hashToken(pair.RefreshToken))
	require.NoError(t, err)
	require.Equal(t, originalFamilyID, original.FamilyID)
	require.Equal(t, original.ExpiresAt.Unix(), pair.RefreshExpiresAt)
	require.Equal(t, 9*24*time.Hour, original.ExpiresAt.Sub(original.CreatedAt))
	claims := accountCenterRefreshClaims(t, s, pair.AccessToken)
	require.NotEmpty(t, claims.AccountGeneration)
	require.Equal(t, claims.AccountGeneration, original.AccountGeneration)
	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	var persisted RefreshTokenData
	require.NoError(t, json.Unmarshal(encoded, &persisted))
	require.Equal(t, original.AccountGeneration, persisted.AccountGeneration)
	require.Contains(t, string(encoded), `"ag":"`+claims.AccountGeneration+`"`)

	for range 2 {
		next, err := s.RefreshTokenPair(ctx, pair.RefreshToken)
		require.NoError(t, err)
		require.NotEqual(t, pair.RefreshToken, next.RefreshToken)
		stored, err := cache.GetRefreshToken(ctx, hashToken(next.RefreshToken))
		require.NoError(t, err)
		require.Equal(t, stored.ExpiresAt.Unix(), next.RefreshExpiresAt)
		require.Equal(t, original.AccountGeneration, stored.AccountGeneration)
		require.Equal(t, original.SessionEpoch, stored.SessionEpoch)
		require.Equal(t, original.FamilyID, stored.FamilyID)
		nextClaims := accountCenterRefreshClaims(t, s, next.AccessToken)
		require.Equal(t, claims.AccountGeneration, nextClaims.AccountGeneration)
		require.Equal(t, original.FamilyID, nextClaims.SessionID)
		require.Equal(t, original.SessionEpoch, nextClaims.SessionEpoch)
		_, err = s.RefreshTokenPair(ctx, pair.RefreshToken)
		require.ErrorIs(t, err, ErrRefreshTokenInvalid)
		pair = &next.TokenPair
	}
	require.Equal(t, 3, cache.stores)
}

func TestAccountCenterRefresh_SecurityChangeRejectedBeforeConsume(t *testing.T) {
	changes := map[string]func(*User){
		"enable factor":  func(u *User) { u.TotpEnabled = true },
		"replace factor": func(u *User) { secret := "replacement-encrypted-factor"; u.TotpSecretEncrypted = &secret },
		"password":       func(u *User) { u.PasswordHash = "replacement-password-hash" },
		"email":          func(u *User) { u.Email = "changed@example.test" },
		"token version":  func(u *User) { u.TokenVersion++ },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, _, cache, user := epochTestService()
			s.cfg.AccountCenter.Enabled = true
			secret := "original-encrypted-factor"
			user.TotpSecretEncrypted = &secret
			pair, err := s.GenerateTokenPair(ctx, user, "")
			require.NoError(t, err)
			consumed := false
			cache.beforeConsume = func() { consumed = true }
			change(user)
			next, err := s.RefreshTokenPair(ctx, pair.RefreshToken)
			require.ErrorIs(t, err, ErrTokenRevoked)
			require.Nil(t, next)
			require.False(t, consumed, "reject a stale proof before atomic consumption")
			require.Equal(t, 1, cache.stores)
			_, err = cache.GetRefreshToken(ctx, hashToken(pair.RefreshToken))
			require.NoError(t, err)
			_, err = s.RefreshToken(ctx, pair.AccessToken)
			require.ErrorIs(t, err, ErrTokenRevoked, "legacy access refresh must not acquire a newer generation")
		})
	}
}

func TestAccountCenterRefresh_ConcurrentSecurityChangeKeepsValidatedSnapshot(t *testing.T) {
	ctx := context.Background()
	s, _, cache, user := epochTestService()
	s.cfg.AccountCenter.Enabled = true
	pair, err := s.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	original := accountCenterRefreshClaims(t, s, pair.AccessToken).AccountGeneration
	cache.beforeConsume = func() { user.TotpEnabled = true }
	next, err := s.RefreshTokenPair(ctx, pair.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, original, accountCenterRefreshClaims(t, s, next.AccessToken).AccountGeneration)
	stored, err := cache.GetRefreshToken(ctx, hashToken(next.RefreshToken))
	require.NoError(t, err)
	require.Equal(t, original, stored.AccountGeneration)
	require.NotEqual(t, accountCenterGeneration(user, stored.SessionEpoch), stored.AccountGeneration)
	cache.beforeConsume = nil
	_, err = s.RefreshTokenPair(ctx, next.RefreshToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	require.Equal(t, 2, cache.stores)
}

func TestAccountCenterRefresh_DisabledPreservesLegacyJSONAndFactorBehavior(t *testing.T) {
	ctx := context.Background()
	s, _, cache, user := epochTestService()
	pair, err := s.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)
	for range 2 {
		stored, err := cache.GetRefreshToken(ctx, hashToken(pair.RefreshToken))
		require.NoError(t, err)
		require.Empty(t, stored.AccountGeneration)
		claims := accountCenterRefreshClaims(t, s, pair.AccessToken)
		require.Empty(t, claims.AccountGeneration)
		require.Zero(t, pair.RefreshExpiresAt)
		for _, value := range []any{stored, claims, pair} {
			encoded, err := json.Marshal(value)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), `"ag"`)
			require.NotContains(t, string(encoded), `"refresh_expires_at"`)
		}
		user.TotpEnabled = !user.TotpEnabled
		next, err := s.RefreshTokenPair(ctx, pair.RefreshToken)
		require.NoError(t, err)
		pair = &next.TokenPair
	}
}

func TestAccountCenterRefresh_LegacyRecordAndHelperRemainCompatible(t *testing.T) {
	ctx := context.Background()
	s, _, cache, user := epochTestService()
	token, err := s.generateRefreshToken(ctx, user, "legacy-family", "")
	require.NoError(t, err)
	original, err := cache.GetRefreshToken(ctx, hashToken(token))
	require.NoError(t, err)
	require.Empty(t, original.AccountGeneration)
	s.cfg.AccountCenter.Enabled = true
	next, err := s.RefreshTokenPair(ctx, token)
	require.NoError(t, err)
	stored, err := cache.GetRefreshToken(ctx, hashToken(next.RefreshToken))
	require.NoError(t, err)
	require.Equal(t, original.FamilyID, stored.FamilyID)
	require.NotEmpty(t, stored.AccountGeneration)
	require.Equal(t, stored.ExpiresAt.Unix(), next.RefreshExpiresAt)
}

type accountCenterFailingRefreshCache struct {
	*epochRefreshCache
	readErr, storeErr error
}

func (c *accountCenterFailingRefreshCache) GetRefreshToken(ctx context.Context, hash string) (*RefreshTokenData, error) {
	if c.readErr != nil {
		return nil, c.readErr
	}
	return c.epochRefreshCache.GetRefreshToken(ctx, hash)
}
func (c *accountCenterFailingRefreshCache) StoreRefreshToken(ctx context.Context, hash string, data *RefreshTokenData, ttl time.Duration) error {
	if c.storeErr != nil {
		return c.storeErr
	}
	return c.epochRefreshCache.StoreRefreshToken(ctx, hash, data, ttl)
}

func TestAccountCenterRefresh_CacheFailuresNeverReturnSuccessor(t *testing.T) {
	for _, failure := range []string{"read", "consume", "store"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			s, _, cache, user := epochTestService()
			s.cfg.AccountCenter.Enabled = true
			failing := &accountCenterFailingRefreshCache{epochRefreshCache: cache}
			s.refreshTokenCache = failing
			pair, err := s.GenerateTokenPair(ctx, user, "")
			require.NoError(t, err)
			unavailable := errors.New("cache unavailable")
			switch failure {
			case "read":
				failing.readErr = unavailable
			case "consume":
				cache.consumeErr = unavailable
			case "store":
				failing.storeErr = unavailable
			}
			next, err := s.RefreshTokenPair(ctx, pair.RefreshToken)
			require.Error(t, err)
			require.Nil(t, next)
			if failure != "store" {
				require.ErrorIs(t, err, ErrServiceUnavailable)
			}
			require.Equal(t, 1, cache.stores)
		})
	}
}
