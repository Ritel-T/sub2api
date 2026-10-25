//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDeepSeekToolsPoolContextPreservesCallerBillingIdentity(t *testing.T) {
	caller := &Group{ID: 17, Platform: PlatformDeepseek}
	pool := &Group{ID: 12, Platform: PlatformOpenAI, Status: StatusActive}
	repo := &mockGroupRepoForGateway{groups: map[int64]*Group{12: pool}}
	s := &OpenAIGatewayService{channelService: &ChannelService{groupRepo: repo}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, caller)
	key := &APIKey{GroupID: &caller.ID, Group: caller}
	selected, err := s.DeepSeekToolsSchedulingContext(ctx, 12)
	require.NoError(t, err)
	require.Same(t, caller, selected.Value(ctxkey.Group))
	require.Same(t, caller, ctx.Value(ctxkey.Group))
	require.Same(t, caller, key.Group)
	require.Equal(t, int64(17), *key.GroupID)
	require.Equal(t, PlatformDeepseek, QuotaPlatform(ctx, key))
	pool.Platform = PlatformDeepseek
	_, err = s.DeepSeekToolsSchedulingContext(ctx, 12)
	require.Error(t, err)
}
