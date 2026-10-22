//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAITokenProvider_CacheMissDoesNotWarnButRedisFailureDoes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cacheError error
		warn       bool
	}{
		{"empty_miss", nil, false},
		{"cache_miss", ErrTokenCacheMiss, false},
		{"wrapped_cache_miss", fmt.Errorf("cache get: %w", ErrTokenCacheMiss), false},
		{"redis_unavailable", errors.New("connection refused"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
			defer slog.SetDefault(previous)
			cache := newOpenAITokenCacheStub()
			cache.getErr = tc.cacheError
			account := &Account{
				ID: 117, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "credential-token",
					"expires_at":   time.Now().Add(time.Hour).Format(time.RFC3339),
				},
			}
			token, err := NewOpenAITokenProvider(nil, cache, nil).GetAccessToken(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, "credential-token", token)
			require.Equal(t, "credential-token", cache.tokens[OpenAITokenCacheKey(account)])
			if tc.warn {
				require.Contains(t, output.String(), "openai_token_cache_get_failed")
				require.Contains(t, output.String(), "level=WARN")
			} else {
				require.NotContains(t, output.String(), "openai_token_cache_get_failed")
			}
		})
	}
}
