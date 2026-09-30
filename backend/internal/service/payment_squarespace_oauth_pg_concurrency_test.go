//go:build integration

package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// The fixture owns a new PostgreSQL 18 container, with no network and no exposed
// TCP port. It connects through its own Unix socket, never an environment DSN.
// Only synthetic OAuth state rows are inserted; no users, orders or balances.
func squarespaceOAuthPGFixture(t *testing.T) (*PaymentConfigService, *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	socketDir := filepath.Join(t.TempDir(), "pg-socket")
	require.NoError(t, os.Mkdir(socketDir, 0777))
	require.NoError(t, os.Chmod(socketDir, 0777))
	name := "sq-oauth-pg-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	args := []string{"run", "--detach", "--pull=never", "--name", name,
		"--network", "none", "--tmpfs", "/var/lib/postgresql:rw,nosuid,size=256m",
		"--mount", "type=bind,source=" + socketDir + ",target=/var/run/postgresql",
		"--env", "POSTGRES_HOST_AUTH_METHOD=trust", "--env", "POSTGRES_USER=sq_oauth_fixture",
		"--env", "POSTGRES_DB=sq_oauth_fixture", "postgres:18.1-alpine3.23",
		"-c", "listen_addresses=", "-c", "unix_socket_permissions=0777"}
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("isolated PG18 fixture could not start: %v", err)
	}
	id := strings.TrimSpace(string(output))
	require.Regexp(t, `^[0-9a-f]{64}$`, id)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if err := exec.CommandContext(cleanupCtx, "docker", "rm", "--force", id).Run(); err != nil {
			t.Errorf("could not remove owned PostgreSQL fixture: %v", err)
		}
	})
	mode, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.HostConfig.NetworkMode}}", id).Output()
	require.NoError(t, err)
	require.Equal(t, "none", strings.TrimSpace(string(mode)))
	db, err := sql.Open("postgres", "host="+socketDir+" dbname=sq_oauth_fixture user=sq_oauth_fixture sslmode=disable TimeZone=UTC connect_timeout=1")
	require.NoError(t, err)
	db.SetMaxOpenConns(10)
	require.Eventually(t, func() bool {
		pingCtx, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		return db.PingContext(pingCtx) == nil
	}, 30*time.Second, 100*time.Millisecond, "owned Unix-socket PG fixture must become ready")
	// The image adjusts directory ownership at startup. Keep its owned socket
	// mount removable by the test runner after container teardown.
	require.NoError(t, exec.CommandContext(ctx, "docker", "exec", id, "chmod", "0777", "/var/run/postgresql").Run())
	var version int
	require.NoError(t, db.QueryRowContext(ctx, "SHOW server_version_num").Scan(&version))
	require.GreaterOrEqual(t, version, 180000)
	require.Less(t, version, 190000)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NoError(t, client.Schema.Create(ctx))
	cfg := NewPaymentConfigService(client, nil, bytes.Repeat([]byte{0x42}, 32))
	t.Log("owned PostgreSQL 18 fixture verified: network=none, Unix socket, no production DSN")
	return cfg, db
}

func squarespaceOAuthPGPair(now time.Time, label string, expired bool) *provider.SquarespaceOAuthTokenPair {
	at := now.Add(time.Hour)
	if expired {
		at = now.Add(-time.Minute)
	}
	return &provider.SquarespaceOAuthTokenPair{AccessToken: label + "_access", RefreshToken: label + "_refresh",
		AccessTokenExpiresAt: fmt.Sprint(at.Unix()), RefreshTokenExpiresAt: fmt.Sprint(now.Add(7 * 24 * time.Hour).Unix()), TokenType: "bearer"}
}

func squarespaceOAuthPGSeed(t *testing.T, cfg *PaymentConfigService, site string, now time.Time) (*dbent.PaymentSyncState, *provider.SquarespaceOAuthTokenPair) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, cfg.SaveSquarespaceOAuthCredentials(ctx, site, "pg_fixture_client", "original_fixture_secret"))
	old := squarespaceOAuthPGPair(now, site+"_old", true)
	require.NoError(t, cfg.SaveSquarespaceOAuthTokens(ctx, site, "pg_fixture_client", 0, old))
	row, err := cfg.squarespaceSyncState(ctx, site)
	require.NoError(t, err)
	require.Equal(t, int64(1), row.TokenVersion)
	return row, old
}

func squarespaceOAuthPGImport(ctx context.Context, cfg *PaymentConfigService, site string, expected int64, pair *provider.SquarespaceOAuthTokenPair) error {
	tx, err := cfg.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	transactional := *cfg
	transactional.entClient = tx.Client()
	// This seam represents the production transaction after official identity
	// verification. No token-bearing request is sent to Squarespace here.
	if err := transactional.importVerifiedSquarespaceOAuthPair(ctx, site, "pg_fixture_client", "replacement_fixture_secret", expected, pair); err != nil {
		return err
	}
	return tx.Commit()
}

func squarespaceOAuthPGRequireNoBusinessRows(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(`SELECT (SELECT count(*) FROM users) + (SELECT count(*) FROM payment_orders) + (SELECT count(*) FROM redeem_codes) + (SELECT count(*) FROM payment_external_orders) + (SELECT count(*) FROM payment_external_payments) + (SELECT count(*) FROM payment_external_refund_journals)`).Scan(&count))
	require.Zero(t, count, "fixture must not create business records")
}

func TestSquarespaceOAuthPostgresConcurrency(t *testing.T) {
	cfg, db := squarespaceOAuthPGFixture(t)

	t.Run("blocked_http_refresh_rejects_import_then_owner_commits", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		now := time.Now().UTC().Truncate(time.Microsecond)
		site := "pg_active_writer"
		row, old := squarespaceOAuthPGSeed(t, cfg, site, now)
		source, err := cfg.squarespaceTokenSource(ctx, site)
		require.NoError(t, err)
		fresh := squarespaceOAuthPGPair(now, "pg_rotated", false)
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		allowResponse := func() { releaseOnce.Do(func() { close(release) }) }
		defer allowResponse()
		mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(fresh)
		}))
		defer mock.Close()
		mockClient := mock.Client()
		mockClient.Timeout = 20 * time.Second
		source.refresh = func(refreshCtx context.Context, token string) (*provider.SquarespaceOAuthTokenPair, error) {
			if token != old.RefreshToken {
				return nil, fmt.Errorf("fixture refresh token mismatch")
			}
			request, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, mock.URL, nil)
			if err != nil {
				return nil, err
			}
			response, err := mockClient.Do(request)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			var pair provider.SquarespaceOAuthTokenPair
			if err := json.NewDecoder(response.Body).Decode(&pair); err != nil {
				return nil, err
			}
			return &pair, nil
		}
		type refreshResult struct {
			access string
			err    error
		}
		refreshed := make(chan refreshResult, 1)
		go func() { access, err := source.AccessToken(ctx); refreshed <- refreshResult{access, err} }()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("refresh mock HTTP request never entered")
		}
		active, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, "request_in_flight", active.RotationPhase)
		require.Equal(t, int64(1), active.TokenVersion)
		require.NotNil(t, active.RotationStartedAt)

		locker, err := cfg.entClient.Tx(ctx)
		require.NoError(t, err)
		defer func() { _ = locker.Rollback() }()
		locked, err := locker.Client().QueryContext(ctx, "SELECT id FROM payment_sync_states WHERE id=$1 FOR UPDATE", row.ID)
		require.NoError(t, err)
		require.True(t, locked.Next())
		var lockedID int64
		require.NoError(t, locked.Scan(&lockedID))
		require.NoError(t, locked.Close())
		require.Equal(t, row.ID, lockedID)
		imported := make(chan error, 1)
		replacement := squarespaceOAuthPGPair(now, "pg_forbidden_import", false)
		go func() { imported <- squarespaceOAuthPGImport(ctx, cfg, site, 1, replacement) }()
		require.Eventually(t, func() bool {
			var waiting int
			err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%payment_sync_states%' AND query LIKE '%FOR UPDATE%'`).Scan(&waiting)
			return err == nil && waiting > 0
		}, 3*time.Second, 20*time.Millisecond, "Import must acquire a real PostgreSQL row lock")
		select {
		case <-imported:
			t.Fatal("Import escaped the held PostgreSQL row lock")
		default:
		}
		t.Log("observed PostgreSQL FOR UPDATE lock wait in the import transaction")
		require.NoError(t, locker.Rollback())
		select {
		case err = <-imported:
		case <-ctx.Done():
			t.Fatal("Import did not finish after row lock release")
		}
		require.ErrorIs(t, err, ErrSquarespaceTokenRotationBusy)
		unchanged, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
		require.NoError(t, err)
		require.True(t, unchanged.EncryptedOauthCredentials == active.EncryptedOauthCredentials, "rejected import changed encrypted credentials")
		require.True(t, unchanged.EncryptedOauthTokens == active.EncryptedOauthTokens, "rejected import changed encrypted token pair")
		require.Equal(t, active.TokenVersion, unchanged.TokenVersion)
		require.Equal(t, active.RotationPhase, unchanged.RotationPhase)
		require.True(t, unchanged.RotationStartedAt.Equal(*active.RotationStartedAt))
		allowResponse()
		var finished refreshResult
		select {
		case finished = <-refreshed:
		case <-ctx.Done():
			t.Fatal("legitimate refresh owner did not finish")
		}
		require.NoError(t, finished.err)
		require.True(t, finished.access == fresh.AccessToken, "returned token is not the persisted owner's token")
		stored, version, err := cfg.LoadSquarespaceOAuthTokens(ctx, site)
		require.NoError(t, err)
		require.True(t, *stored == *fresh, "owner's complete pair was not persisted")
		require.Equal(t, int64(2), version)
		committed, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, "idle", committed.RotationPhase)
		require.Nil(t, committed.RotationStartedAt)
		require.True(t, committed.EncryptedOauthCredentials == active.EncryptedOauthCredentials)
		squarespaceOAuthPGRequireNoBusinessRows(t, db)
	})

	t.Run("unknown_fresh_import_v2_rejects_stale_owner", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		now := time.Now().UTC().Truncate(time.Microsecond)
		site := "pg_unknown_recovery"
		row, _ := squarespaceOAuthPGSeed(t, cfg, site, now)
		_, err := cfg.entClient.PaymentSyncState.UpdateOneID(row.ID).SetRotationPhase("unknown").SetRotationStartedAt(now).SetLastErrorCode("OAUTH_ROTATION_UNKNOWN").SetRetryAt(now.Add(15 * time.Minute)).Save(ctx)
		require.NoError(t, err)
		fresh := squarespaceOAuthPGPair(now, "pg_recovered", false)
		tx, err := cfg.entClient.Tx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		transactional := *cfg
		transactional.entClient = tx.Client()
		require.NoError(t, transactional.importVerifiedSquarespaceOAuthPair(ctx, site, "pg_fixture_client", "recovered_fixture_secret", 1, fresh))
		beforeCommit, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, int64(1), beforeCommit.TokenVersion, "another PG connection saw an uncommitted imported pair")
		require.Equal(t, "unknown", beforeCommit.RotationPhase)
		require.NoError(t, tx.Commit())
		recovered, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, int64(2), recovered.TokenVersion)
		require.Equal(t, "idle", recovered.RotationPhase)
		require.Nil(t, recovered.RotationStartedAt)
		require.Nil(t, recovered.RetryAt)
		require.Empty(t, recovered.LastErrorCode)
		credentialCipher, tokenCipher := recovered.EncryptedOauthCredentials, recovered.EncryptedOauthTokens
		stale := squarespaceRotationOwner{Version: 1, StartedAt: now}
		attempted := squarespaceOAuthPGPair(now, "pg_stale_clobber", false)
		require.Error(t, cfg.saveRotatedSquarespaceOAuthTokens(ctx, site, "pg_fixture_client", stale, attempted))
		source, err := cfg.squarespaceTokenSource(ctx, site)
		require.NoError(t, err)
		source.markSquarespaceRotationUnknown(row.ID, stale, "OAUTH_ROTATION_UNKNOWN")
		afterStale, err := cfg.entClient.PaymentSyncState.Get(ctx, row.ID)
		require.NoError(t, err)
		require.Equal(t, int64(2), afterStale.TokenVersion)
		require.Equal(t, "idle", afterStale.RotationPhase)
		require.Empty(t, afterStale.LastErrorCode)
		require.True(t, afterStale.EncryptedOauthCredentials == credentialCipher, "stale owner changed recovered credentials")
		require.True(t, afterStale.EncryptedOauthTokens == tokenCipher, "stale owner changed recovered pair")
		stored, version, err := cfg.LoadSquarespaceOAuthTokens(ctx, site)
		require.NoError(t, err)
		require.Equal(t, int64(2), version)
		require.True(t, *stored == *fresh, "stale owner clobbered the v2 pair")
		squarespaceOAuthPGRequireNoBusinessRows(t, db)
		t.Log("PostgreSQL commit visibility and stale-owner CAS rejection verified")
	})
}
