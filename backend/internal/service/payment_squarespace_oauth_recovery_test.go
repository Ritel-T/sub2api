//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentsyncstate"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func squarespaceRecoveryPair(now time.Time, label string, expired bool) *provider.SquarespaceOAuthTokenPair {
	expiry := now.Add(time.Hour)
	if expired {
		expiry = now.Add(-time.Minute)
	}
	return &provider.SquarespaceOAuthTokenPair{AccessToken: label + "_access", RefreshToken: label + "_refresh", AccessTokenExpiresAt: fmt.Sprint(expiry.Unix()), RefreshTokenExpiresAt: fmt.Sprint(now.Add(7 * 24 * time.Hour).Unix()), TokenType: "bearer"}
}
func squarespaceRecoveryFixture(t *testing.T) (*PaymentConfigService, *dbent.PaymentSyncState, *provider.SquarespaceOAuthTokenPair, time.Time) {
	t.Helper()
	bridge, _, _, _, _ := squarespaceBridgeDBFixture(t)
	cfg := bridge.config
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, cfg.SaveSquarespaceOAuthCredentials(ctx, "site_1", "client_id", "original_secret"))
	old := squarespaceRecoveryPair(now, "original", true)
	require.NoError(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site_1", "client_id", 0, old))
	row, err := cfg.squarespaceSyncState(ctx, "site_1")
	require.NoError(t, err)
	return cfg, row, old, now
}
func importSquarespacePairInTestTx(ctx context.Context, cfg *PaymentConfigService, version int64, pair *provider.SquarespaceOAuthTokenPair) error {
	tx, err := cfg.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	transactional := *cfg
	transactional.entClient = tx.Client()
	if err = transactional.importVerifiedSquarespaceOAuthPair(ctx, "site_1", "client_id", "replacement_secret", version, pair); err != nil {
		return err
	}
	return tx.Commit()
}

func TestSquarespaceOAuthUnknownRecoveryRequiresNewPairAndCAS(t *testing.T) {
	cfg, row, old, now := squarespaceRecoveryFixture(t)
	ctx := context.Background()
	row, err := cfg.entClient.PaymentSyncState.UpdateOneID(row.ID).SetRotationPhase("unknown").SetRotationStartedAt(now).SetLastErrorCode("OAUTH_INVALID_RESPONSE_HTTP_200").SetRetryAt(now.Add(15 * time.Minute)).Save(ctx)
	require.NoError(t, err)
	source, err := cfg.squarespaceTokenSource(ctx, "site_1")
	require.NoError(t, err)
	calls := 0
	source.refresh = func(context.Context, string) (*provider.SquarespaceOAuthTokenPair, error) {
		calls++
		return nil, errors.New("must not replay")
	}
	_, err = source.AccessToken(ctx)
	require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
	require.Zero(t, calls)
	reused := squarespaceRecoveryPair(now, "fresh", false)
	reused.RefreshToken = old.RefreshToken
	require.ErrorIs(t, importSquarespacePairInTestTx(ctx, cfg, 1, reused), ErrSquarespaceReauthorizationRequired)
	fresh := squarespaceRecoveryPair(now, "fresh", false)
	require.Error(t, importSquarespacePairInTestTx(ctx, cfg, 0, fresh))
	require.NoError(t, importSquarespacePairInTestTx(ctx, cfg, 1, fresh))
	stored, version, err := cfg.LoadSquarespaceOAuthTokens(ctx, "site_1")
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
	require.Equal(t, *fresh, *stored)
	state, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, "idle", state.RotationPhase)
	require.Nil(t, state.RotationStartedAt)
	require.Nil(t, state.RetryAt)
	require.Empty(t, state.LastErrorCode)
	access, err := source.AccessToken(ctx)
	require.NoError(t, err)
	require.Equal(t, fresh.AccessToken, access)
	require.Zero(t, calls)
	require.Error(t, cfg.saveRotatedSquarespaceOAuthTokens(ctx, "site_1", "client_id", squarespaceRotationOwner{Version: 1, StartedAt: now}, squarespaceRecoveryPair(now, "stale_owner", false)))
	stored, version, err = cfg.LoadSquarespaceOAuthTokens(ctx, "site_1")
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
	require.Equal(t, fresh.RefreshToken, stored.RefreshToken)
}
func TestSquarespaceOAuthImportCannotStealActiveOrStaleWriter(t *testing.T) {
	for _, age := range []time.Duration{0, 10 * time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			cfg, row, old, now := squarespaceRecoveryFixture(t)
			ctx := context.Background()
			started := now.Add(-age)
			state, err := cfg.entClient.PaymentSyncState.UpdateOneID(row.ID).SetRotationPhase("request_in_flight").SetRotationStartedAt(started).Save(ctx)
			require.NoError(t, err)
			encryptedCredentials := state.EncryptedOauthCredentials
			encryptedTokens := state.EncryptedOauthTokens
			fresh := squarespaceRecoveryPair(now, "fresh", false)
			require.ErrorIs(t, importSquarespacePairInTestTx(ctx, cfg, 1, fresh), ErrSquarespaceTokenRotationBusy)
			require.ErrorIs(t, cfg.SaveSquarespaceOAuthTokens(ctx, "site_1", "client_id", 1, fresh), ErrSquarespaceTokenRotationBusy)
			state, err = cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, int64(1), state.TokenVersion)
			require.Equal(t, "request_in_flight", state.RotationPhase)
			require.Equal(t, encryptedCredentials, state.EncryptedOauthCredentials)
			require.Equal(t, encryptedTokens, state.EncryptedOauthTokens)
			require.ErrorIs(t, cfg.saveRotatedSquarespaceOAuthTokens(ctx, "site_1", "client_id", squarespaceRotationOwner{Version: 1, StartedAt: started.Add(time.Microsecond)}, fresh), ErrSquarespaceTokenRotationBusy)
			require.NoError(t, cfg.saveRotatedSquarespaceOAuthTokens(ctx, "site_1", "client_id", squarespaceRotationOwner{Version: 1, StartedAt: *state.RotationStartedAt}, fresh))
			stored, version, err := cfg.LoadSquarespaceOAuthTokens(ctx, "site_1")
			require.NoError(t, err)
			require.Equal(t, int64(2), version)
			require.Equal(t, *fresh, *stored)
			require.NotEqual(t, old.RefreshToken, stored.RefreshToken)
		})
	}
}
func TestSquarespaceOAuthRefreshFailureKeepsSafeDiagnosisAndNeverReplays(t *testing.T) {
	for _, failure := range []error{errors.New("must-never-expose-secret-canary"), &provider.SquarespaceAPIError{Kind: "invalid_oauth_response", HTTPStatus: 200}} {
		t.Run(provider.SquarespaceOAuthFailureCode(failure), func(t *testing.T) {
			cfg, row, _, _ := squarespaceRecoveryFixture(t)
			ctx := context.Background()
			source, err := cfg.squarespaceTokenSource(ctx, "site_1")
			require.NoError(t, err)
			calls := 0
			source.refresh = func(context.Context, string) (*provider.SquarespaceOAuthTokenPair, error) {
				calls++
				return nil, failure
			}
			_, err = source.AccessToken(ctx)
			require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
			require.NotContains(t, err.Error(), "secret-canary")
			state, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
			require.NoError(t, err)
			expected := provider.SquarespaceOAuthFailureCode(failure)
			require.Equal(t, "unknown", state.RotationPhase)
			require.Equal(t, expected, state.LastErrorCode)
			require.LessOrEqual(t, len(state.LastErrorCode), 64)
			bridge := NewSquarespacePaymentBridge(&PaymentService{entClient: source.config.entClient, configService: source.config}, nil, nil)
			_ = bridge.deferSync(ctx, row.ID, errors.New("generic downstream wrapper must-never-expose-secret-canary"))
			state, err = cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, expected, state.LastErrorCode)
			_, err = source.AccessToken(ctx)
			require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
			require.Equal(t, 1, calls)
		})
	}
}
func TestSquarespaceOAuthUnpersistedPairNeverUsedAndBecomesUnknown(t *testing.T) {
	cfg, row, _, now := squarespaceRecoveryFixture(t)
	source, err := cfg.squarespaceTokenSource(context.Background(), "site_1")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	fresh := squarespaceRecoveryPair(now, "new_unpersisted", false)
	source.refresh = func(context.Context, string) (*provider.SquarespaceOAuthTokenPair, error) {
		cancel()
		return fresh, nil
	}
	access, err := source.AccessToken(ctx)
	require.Empty(t, access)
	require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
	state, err := cfg.entClient.PaymentSyncState.Get(context.Background(), row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.TokenVersion)
	require.Equal(t, "unknown", state.RotationPhase)
	require.Equal(t, "OAUTH_ROTATION_PERSISTENCE_FAILED", state.LastErrorCode)
	require.NotContains(t, state.EncryptedOauthTokens, strings.TrimSuffix(fresh.AccessToken, "_access"))
}
func TestSquarespaceOAuthOldOwnerFailureCannotMarkRecoveredPairUnknown(t *testing.T) {
	cfg, row, _, now := squarespaceRecoveryFixture(t)
	ctx := context.Background()
	_, err := cfg.entClient.PaymentSyncState.Update().Where(paymentsyncstate.IDEQ(row.ID)).SetRotationPhase("unknown").SetRotationStartedAt(now).Save(ctx)
	require.NoError(t, err)
	fresh := squarespaceRecoveryPair(now, "recovered", false)
	require.NoError(t, importSquarespacePairInTestTx(ctx, cfg, 1, fresh))
	source, err := cfg.squarespaceTokenSource(ctx, "site_1")
	require.NoError(t, err)
	source.markSquarespaceRotationUnknown(row.ID, squarespaceRotationOwner{Version: 1, StartedAt: now}, "OAUTH_ROTATION_UNKNOWN")
	state, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), state.TokenVersion)
	require.Equal(t, "idle", state.RotationPhase)
	require.Empty(t, state.LastErrorCode)
}

func TestSquarespaceOAuthLeaseReadFailureBeforeHTTPDoesNotStrandActiveState(t *testing.T) {
	cfg, row, _, now := squarespaceRecoveryFixture(t)
	ctx := context.Background()
	source, err := cfg.squarespaceTokenSource(ctx, "site_1")
	require.NoError(t, err)
	reads := 0
	cfg.entClient.PaymentSyncState.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return squarespaceRecoveryQuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			reads++
			if reads == 3 {
				return nil, errors.New("controlled ownership read failure")
			}
			return next.Query(ctx, query)
		})
	}))
	calls := 0
	source.refresh = func(context.Context, string) (*provider.SquarespaceOAuthTokenPair, error) {
		calls++
		return squarespaceRecoveryPair(now, "not_called", false), nil
	}
	access, err := source.AccessToken(ctx)
	require.Empty(t, access)
	require.ErrorIs(t, err, ErrSquarespaceReauthorizationRequired)
	require.Zero(t, calls)
	state, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
	require.NoError(t, err)
	require.Equal(t, "unknown", state.RotationPhase)
	require.Equal(t, "OAUTH_ROTATION_LEASE_READ_FAILED", state.LastErrorCode)
	require.NoError(t, importSquarespacePairInTestTx(ctx, cfg, 1, squarespaceRecoveryPair(now, "fresh_recovery", false)))
}

type squarespaceRecoveryQuerierFunc func(context.Context, ent.Query) (ent.Value, error)

func (f squarespaceRecoveryQuerierFunc) Query(ctx context.Context, query ent.Query) (ent.Value, error) {
	return f(ctx, query)
}
