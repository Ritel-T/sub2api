package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type balanceAttemptRepository interface {
	ClaimBalanceEmailAttempt(context.Context, string, string) (bool, error)
	ReleaseBalanceEmailAttempt(context.Context, string, string) (bool, error)
}

func TestBalanceNotifyAttemptRepository_UniqueClaimAndOwnerRelease(t *testing.T) {
	client := newSecuritySecretTestClient(t)
	first, firstOK := NewSettingRepository(client).(balanceAttemptRepository)
	require.True(t, firstOK)
	peer, peerOK := NewSettingRepository(client).(balanceAttemptRepository)
	require.True(t, peerOK)
	ctx := context.Background()
	ok, err := first.ClaimBalanceEmailAttempt(ctx, "balance_email_attempt:test", "first")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = peer.ClaimBalanceEmailAttempt(ctx, "balance_email_attempt:test", "peer")
	require.NoError(t, err)
	require.False(t, ok)
	owner, err := NewSettingRepository(client).GetValue(ctx, "balance_email_attempt:test")
	require.NoError(t, err)
	require.Equal(t, "first", owner)
	ok, err = peer.ReleaseBalanceEmailAttempt(ctx, "balance_email_attempt:test", "peer")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = first.ReleaseBalanceEmailAttempt(ctx, "balance_email_attempt:test", "first")
	require.NoError(t, err)
	require.True(t, ok)
	_, err = NewSettingRepository(client).GetValue(ctx, "balance_email_attempt:test")
	require.ErrorIs(t, err, service.ErrSettingNotFound)
	ok, err = peer.ClaimBalanceEmailAttempt(ctx, "balance_email_attempt:test", "peer")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = first.ReleaseBalanceEmailAttempt(ctx, "balance_email_attempt:test", "first")
	require.NoError(t, err)
	require.False(t, ok)
	owner, err = NewSettingRepository(client).GetValue(ctx, "balance_email_attempt:test")
	require.NoError(t, err)
	require.Equal(t, "peer", owner)
}
