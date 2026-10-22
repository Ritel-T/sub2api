//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type epochSettingsRepo struct {
	SettingRepository
	mu                sync.Mutex
	values            map[string]string
	readErr, writeErr error
}

func (r *epochSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr != nil {
		return "", r.readErr
	}
	v, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (r *epochSettingsRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	r.values[key] = value
	return nil
}

type epochUserRepo struct {
	UserRepository
	user *User
}

func (r *epochUserRepo) GetByID(context.Context, int64) (*User, error) {
	u := *r.user
	return &u, nil
}

type epochRefreshCache struct {
	RefreshTokenCache
	mu                     sync.Mutex
	tokens                 map[string]*RefreshTokenData
	consumeErr, cleanupErr error
	stores                 int
	beforeConsume          func()
}

func (c *epochRefreshCache) StoreRefreshToken(_ context.Context, hash string, data *RefreshTokenData, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := *data
	c.tokens[hash] = &d
	c.stores++
	return nil
}
func (c *epochRefreshCache) GetRefreshToken(_ context.Context, hash string) (*RefreshTokenData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.tokens[hash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	v := *d
	return &v, nil
}
func (c *epochRefreshCache) ConsumeRefreshToken(_ context.Context, hash string) (bool, error) {
	if c.beforeConsume != nil {
		c.beforeConsume()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumeErr != nil {
		return false, c.consumeErr
	}
	if _, ok := c.tokens[hash]; !ok {
		return false, nil
	}
	delete(c.tokens, hash)
	return true, nil
}
func (c *epochRefreshCache) DeleteUserRefreshTokens(_ context.Context, _ int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cleanupErr != nil {
		return c.cleanupErr
	}
	c.tokens = make(map[string]*RefreshTokenData)
	return nil
}
func (c *epochRefreshCache) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}
func (c *epochRefreshCache) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}
func (c *epochRefreshCache) DeleteTokenFamily(context.Context, string) error { return nil }

func epochTestService() (*AuthService, *epochSettingsRepo, *epochRefreshCache, *User) {
	r := &epochSettingsRepo{values: make(map[string]string)}
	c := &epochRefreshCache{tokens: make(map[string]*RefreshTokenData)}
	u := &User{ID: 42, Email: "user@example.test", PasswordHash: "unchanged-hash", Role: RoleUser, Status: StatusActive}
	s := &AuthService{
		userRepo: &epochUserRepo{user: u}, refreshTokenCache: c,
		settingService: &SettingService{settingRepo: r},
		cfg:            &config.Config{JWT: config.JWTConfig{Secret: "test-epoch-signing-key", ExpireHour: 1, RefreshTokenExpireDays: 7}},
	}
	return s, r, c, u
}

func TestAuthSessionEpoch_RevokePersistsAndRejectsAccessAndRefresh(t *testing.T) {
	s, r, c, u := epochTestService()
	ctx := context.Background()
	pair, err := s.GenerateTokenPair(ctx, u, "")
	require.NoError(t, err)
	_, err = s.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	require.NoError(t, s.RevokeAllUserTokens(ctx, u.ID))
	_, err = s.ValidateToken(pair.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	_, err = s.RefreshToken(ctx, pair.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	_, err = s.RefreshTokenPair(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, ErrRefreshTokenInvalid)
	// New AuthService process uses the same durable repository, not local state.
	restarted := *s
	restarted.settingService = &SettingService{settingRepo: r}
	_, err = restarted.ValidateToken(pair.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	fresh, err := restarted.GenerateTokenPair(ctx, u, "")
	require.NoError(t, err)
	_, err = restarted.ValidateToken(fresh.AccessToken)
	require.NoError(t, err)
	_, err = restarted.RefreshTokenPair(ctx, fresh.RefreshToken)
	require.NoError(t, err)
	require.NotEmpty(t, r.values[sessionEpochSettingKey(u.ID)])
	require.Equal(t, "unchanged-hash", u.PasswordHash)
	require.Greater(t, c.stores, 1)
}

func TestAuthSessionEpoch_CleanupFailureStillRevokesAndReturnsError(t *testing.T) {
	s, _, c, u := epochTestService()
	ctx := context.Background()
	pair, err := s.GenerateTokenPair(ctx, u, "")
	require.NoError(t, err)
	c.cleanupErr = errors.New("redis unavailable")
	require.Error(t, s.RevokeAllUserTokens(ctx, u.ID))
	_, err = s.ValidateToken(pair.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	_, err = s.RefreshTokenPair(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestAuthSessionEpoch_StorageFailuresFailClosed(t *testing.T) {
	s, r, _, u := epochTestService()
	ctx := context.Background()
	token, err := s.GenerateToken(ctx, u)
	require.NoError(t, err)
	r.readErr = errors.New("db unavailable")
	_, err = s.ValidateToken(token)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	_, err = s.GenerateToken(ctx, u)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	r.readErr = nil
	r.writeErr = errors.New("write failed")
	require.Error(t, s.RevokeAllUserTokens(ctx, u.ID))
	_, err = s.ValidateToken(token)
	require.NoError(t, err)
	r.writeErr = nil
	r.values[sessionEpochSettingKey(u.ID)] = "corrupt"
	_, err = s.ValidateToken(token)
	require.ErrorIs(t, err, ErrServiceUnavailable)
}

func TestAuthSessionEpoch_RefreshRacingRevokeCannotMintNewEpoch(t *testing.T) {
	s, _, c, u := epochTestService()
	ctx := context.Background()
	pair, err := s.GenerateTokenPair(ctx, u, "")
	require.NoError(t, err)
	// Leave the old refresh record to exercise an already validated refresh.
	c.cleanupErr = errors.New("redis cleanup unavailable")
	c.beforeConsume = func() { require.Error(t, s.RevokeAllUserTokens(ctx, u.ID)) }
	raced, err := s.RefreshTokenPair(ctx, pair.RefreshToken)
	require.NoError(t, err)
	_, err = s.ValidateToken(raced.AccessToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	c.beforeConsume = nil
	_, err = s.RefreshTokenPair(ctx, raced.RefreshToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestAuthRefresh_ConsumeFailureNeverIssuesSuccessor(t *testing.T) {
	s, _, c, u := epochTestService()
	ctx := context.Background()
	pair, err := s.GenerateTokenPair(ctx, u, "")
	require.NoError(t, err)
	c.consumeErr = errors.New("unknown redis result")
	_, err = s.RefreshTokenPair(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.Equal(t, 1, c.stores)
}

func TestAuthRefresh_ConcurrentRequestsIssueOneSuccessor(t *testing.T) {
	s, _, c, u := epochTestService()
	ctx := context.Background()
	pair, err := s.GenerateTokenPair(ctx, u, "")
	require.NoError(t, err)
	var arrived sync.WaitGroup
	arrived.Add(16)
	c.beforeConsume = func() { arrived.Done(); arrived.Wait() }
	results := make(chan error, 16)
	for range 16 {
		go func() { _, err := s.RefreshTokenPair(ctx, pair.RefreshToken); results <- err }()
	}
	success := 0
	for range 16 {
		err := <-results
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrRefreshTokenInvalid)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 2, c.stores, "initial token and exactly one successor")
}

func TestAuthSessionEpoch_ExpiredForgedSignatureCannotRefresh(t *testing.T) {
	s, _, _, u := epochTestService()
	claims := JWTClaims{UserID: u.ID, TokenVersion: resolvedTokenVersion(u), RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString([]byte("attacker-key"))
	require.NoError(t, err)
	_, err = s.RefreshToken(context.Background(), token)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestAuthSessionEpoch_ExpiredFutureTokenCannotRefresh(t *testing.T) {
	s, _, _, u := epochTestService()
	claims := JWTClaims{UserID: u.ID, TokenVersion: resolvedTokenVersion(u), RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString([]byte(s.cfg.JWT.Secret))
	require.NoError(t, err)
	_, err = s.RefreshToken(context.Background(), token)
	require.ErrorIs(t, err, ErrInvalidToken)
}
