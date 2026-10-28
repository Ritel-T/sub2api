//go:build unit

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Explicitly targeted at a disposable local PostgreSQL fixture. Every table is
// session-local; no production configuration or usage record is accessed.
func TestAccountWindowStandard_ExactRangeStoredPricesPostgres(t *testing.T) {
	dsn := os.Getenv("SUB2API_WINDOW_STANDARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUB2API_WINDOW_STANDARD_TEST_DSN to the disposable local PostgreSQL fixture")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `CREATE TEMP TABLE usage_logs (
 account_id bigint, created_at timestamptz, total_cost numeric DEFAULT 0,
 actual_cost numeric DEFAULT 0, account_stats_cost numeric, account_rate_multiplier numeric,
 model text DEFAULT 'gpt-6-astra', upstream_model text, upstream_response_model text,
 input_tokens bigint DEFAULT 0, output_tokens bigint DEFAULT 0,
 cache_creation_tokens bigint DEFAULT 0, cache_read_tokens bigint DEFAULT 0,
 cache_creation_5m_tokens bigint DEFAULT 0, cache_creation_1h_tokens bigint DEFAULT 0,
 image_input_tokens bigint DEFAULT 0, image_output_tokens bigint DEFAULT 0,
 image_count bigint DEFAULT 0, video_count bigint DEFAULT 0,
 billing_mode text, service_tier text
 ); CREATE INDEX ON usage_logs(account_id,created_at);
 INSERT INTO usage_logs(account_id,created_at,total_cost,actual_cost,account_stats_cost,account_rate_multiplier,input_tokens) VALUES
 (42,'2026-10-28T09:59:59Z',500,500,500,1,1000000),
 (42,'2026-10-28T10:00:00Z',12.34,1.23,40,3,1000000),
 (42,'2026-10-28T11:00:00Z',600,600,600,1,1000000),
 (43,'2026-10-28T10:30:00Z',999,999,999,1,1000000);
 INSERT INTO usage_logs(account_id,created_at,total_cost,actual_cost,account_stats_cost,account_rate_multiplier,input_tokens,model) VALUES
 (42,'2026-10-28T10:15:00Z',2,0.20,2,1,1000000,'missing-catalog-model'),
 (42,'2026-10-28T10:30:00Z',1.11,0.50,1.11,1,10,'codex-auto-review');`)
	require.NoError(t, err)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	stats, err := repo.GetAccountWindowStatsRange(ctx, 42, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.Requests)
	require.InDelta(t, 15.45, stats.StandardCost, 0.0000001)
	require.InDelta(t, 123.11, stats.Cost, 0.0000001)
	require.InDelta(t, 1.93, stats.UserCost, 0.0000001)
	require.NotNil(t, stats.APIEquivalentCost)
	require.InDelta(t, 21.11, *stats.APIEquivalentCost, 0.0000001)
	require.Equal(t, int64(1), stats.APIEquivalentUnpricedRequests)
	require.Equal(t, int64(1), stats.APIEquivalentInternalPricedRequests)
	// Empty future cycles are known zero, not inferred from quota utilization.
	empty, err := repo.GetAccountWindowStatsRange(ctx, 42, end.Add(time.Hour), end.Add(2*time.Hour))
	require.NoError(t, err)
	require.Zero(t, empty.StandardCost)
	require.Zero(t, empty.Requests)
}
