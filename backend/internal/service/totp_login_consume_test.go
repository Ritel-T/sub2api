//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Embedding the old interface deliberately verifies that no new method is
// required from existing cache stubs. Any non-consume operation would panic.
type totpLoginLegacyCache struct{ TotpCache }

type totpLoginConsumeCache struct {
	TotpCache
	ctx     context.Context
	token   string
	calls   int
	session *TotpLoginSession
	err     error
}

func (c *totpLoginConsumeCache) ConsumeLoginSession(ctx context.Context, token string) (*TotpLoginSession, error) {
	c.ctx, c.token = ctx, token
	c.calls++
	return c.session, c.err
}

func TestTotpConsumeLoginSessionRequiresAtomicCache(t *testing.T) {
	for _, svc := range []*TotpService{nil, {}, {cache: &totpLoginLegacyCache{}}} {
		session, err := svc.ConsumeLoginSession(context.Background(), "private-challenge")
		require.Nil(t, session)
		require.ErrorIs(t, err, ErrServiceUnavailable)
	}
}

func TestTotpConsumeLoginSessionPreservesChallengeAndProof(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	want := &TotpLoginSession{
		UserID: 42, Email: "user@example.com", TokenExpiry: time.Now().Add(time.Minute),
		Authentication:   NativeAuthenticationProof{Methods: []string{"pwd"}, AuthTime: 123, Generation: "generation", Epoch: "epoch"},
		PendingOAuthBind: &PendingOAuthBindLoginSession{PendingSessionToken: "pending", BrowserSessionKey: "browser"},
	}
	cache := &totpLoginConsumeCache{session: want}
	svc := NewTotpService(nil, nil, cache, nil, nil, nil)
	got, err := svc.ConsumeLoginSession(ctx, "private-challenge")
	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, 1, cache.calls)
	require.Equal(t, "private-challenge", cache.token)
	require.Same(t, ctx, cache.ctx)
}

func TestTotpConsumeLoginSessionMissing(t *testing.T) {
	cache := &totpLoginConsumeCache{}
	session, err := (&TotpService{cache: cache}).ConsumeLoginSession(context.Background(), "missing")
	require.NoError(t, err)
	require.Nil(t, session)
	require.Equal(t, 1, cache.calls)
}

func TestTotpConsumeLoginSessionUnknownResultFailsClosed(t *testing.T) {
	cache := &totpLoginConsumeCache{
		session: &TotpLoginSession{UserID: 42},
		err:     errors.New("private-token private-secret 654321"),
	}
	session, err := (&TotpService{cache: cache}).ConsumeLoginSession(context.Background(), "private-token")
	require.Nil(t, session)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.NotContains(t, err.Error(), "private-token")
	require.NotContains(t, err.Error(), "private-secret")
	require.NotContains(t, err.Error(), "654321")
	require.Equal(t, 1, cache.calls)
}
