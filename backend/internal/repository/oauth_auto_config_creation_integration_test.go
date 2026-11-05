//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNativeCreationReadyBindsPausedDefaultsInsideTransaction(t *testing.T) {
	ctx := context.Background()
	repo, a, observation := newInitialReadyFixture(t)
	before := nativeRateLimitAccountRow(t, ctx, repo, a.ID)
	beforeOutbox := nativeRateLimitOutboxCount(t, ctx, repo)
	tx, err := repo.client.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	txctx := dbent.NewTxContext(ctx, tx)
	_, err = tx.Client().ExecContext(txctx, "UPDATE accounts SET status='error',schedulable=false WHERE id=$1", a.ID)
	require.NoError(t, err)
	observation.AllowPaused = true
	result, err := repo.MarkGatewayBorrowInitialReadyIfObserved(txctx, a.ID, observation)
	require.NoError(t, err)
	require.True(t, result.Applied)
	current, err := repo.GetByID(txctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusError, current.Status)
	require.False(t, current.Schedulable)
	require.True(t, service.GatewayBorrowInitialReadyMatchesCurrent(current))
	require.Equal(t, true, current.Extra[service.GatewayBorrowAccountQualityPendingKey], "birth fingerprint must not fake quality")
	require.NoError(t, tx.Rollback())
	require.Equal(t, before, nativeRateLimitAccountRow(t, ctx, repo, a.ID))
	require.Equal(t, beforeOutbox, nativeRateLimitOutboxCount(t, ctx, repo), "readiness outbox must rollback with account writes")
}
