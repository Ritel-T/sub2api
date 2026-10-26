package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GETDEL may have succeeded even when its reply was lost. Disable retries for
// this command only, preserving the shared client's ordinary retry policy.
type totpLoginConsumeCmd struct {
	*redis.StringCmd
}

func (*totpLoginConsumeCmd) NoRetry() bool { return true }

// ConsumeLoginSession atomically removes and returns the existing login key.
// Malformed payloads are consumed too and never become reusable challenges.
func (c *TotpCache) ConsumeLoginSession(ctx context.Context, tempToken string) (*service.TotpLoginSession, error) {
	if c == nil || c.rdb == nil {
		return nil, service.ErrServiceUnavailable
	}
	cmd := &totpLoginConsumeCmd{redis.NewStringCmd(ctx, "getdel", totpLoginKeyPrefix+tempToken)}
	if err := c.rdb.Process(ctx, cmd); err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		// Redis errors can include the command/key. Never wrap their text.
		return nil, service.ErrServiceUnavailable
	}
	data, err := cmd.Bytes()
	if err != nil {
		return nil, service.ErrServiceUnavailable
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var session *service.TotpLoginSession
	if err := decoder.Decode(&session); err != nil || session == nil {
		return nil, errors.New("invalid totp login session")
	}
	if err := decoder.Decode(new(any)); err != io.EOF || session.UserID <= 0 || session.TokenExpiry.IsZero() {
		return nil, errors.New("invalid totp login session")
	}
	return session, nil
}
