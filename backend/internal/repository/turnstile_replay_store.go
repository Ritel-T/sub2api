package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	turnstileReplayKeyPrefix = "turnstile:used:v1:"
	// Keep claims longer than Cloudflare's five-minute token validity window.
	turnstileReplayTTL = 10 * time.Minute
)

type turnstileReplayStore struct {
	client *redis.Client
}

func NewTurnstileReplayStore(client *redis.Client) service.TurnstileReplayStore {
	return &turnstileReplayStore{client: client}
}

func (s *turnstileReplayStore) ClaimToken(ctx context.Context, tokenHash string) (bool, error) {
	if s == nil || s.client == nil {
		return false, fmt.Errorf("turnstile replay store is not configured")
	}
	// Redis sees only the digest and a constant marker, never the token itself.
	return s.client.SetNX(ctx, turnstileReplayKeyPrefix+tokenHash, "1", turnstileReplayTTL).Result()
}
