//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClearAccountSchedulingBlockIfUntilProtectsNewer429(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	observedReset := time.Now().Add(20 * time.Minute).UTC().Truncate(time.Second)
	newerReset := observedReset.Add(10 * time.Minute)

	svc.BlockAccountScheduling(account, newerReset, "429")
	require.False(t, svc.ClearAccountSchedulingBlockIfUntil(account.ID, observedReset))
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))

	require.True(t, svc.ClearAccountSchedulingBlockIfUntil(account.ID, newerReset))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}
