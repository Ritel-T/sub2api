package repository

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestUsageReadFiltersLegacyAndAliasedSemantics(t *testing.T) {
	kind := int16(service.RequestTypeWSV2)
	stream := false
	native := true
	bill := int8(1)
	audit := false
	query, args := appendUsageLogReadFilters("SELECT * FROM usage_logs ul WHERE true", nil, usagestats.UsageLogFilters{UserID: 11, APIKeyID: 12, AccountID: 13, GroupID: 14, RequestID: " exact-request ", Model: "client-alias", ModelFilterSource: usagestats.ModelSourceRequested, RequestType: &kind, Stream: &stream, NativeCompactionV2: &native, BillingType: &bill, BillingMode: "image", UpstreamModelMismatch: &audit}, "ul")
	for _, part := range []string{"ul.user_id = $1", "ul.api_key_id = $2", "ul.account_id = $3", "ul.group_id = $4", "ul.request_id = $5", "COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) = $6", "ul.request_type", "ul.openai_ws_mode", "ul.native_compaction_v2", "ul.billing_type", "ul.billing_mode", "COALESCE(ul.image_count, 0) > 0", "ul.upstream_model_mismatch IS FALSE"} {
		require.Contains(t, query, part)
	}
	require.NotContains(t, query, "ul.stream =", "request_type takes precedence over legacy stream just as records")
	require.Equal(t, []any{int64(11), int64(12), int64(13), int64(14), "exact-request", "client-alias", kind, true, int16(1), "image"}, args)
}

// This exercises real query execution against synthetic temporary tables. The DSN
// must point at a disposable local PostgreSQL instance; no application data is read.
func TestUsageSharedFiltersPostgresParity(t *testing.T) {
	dsn := os.Getenv("SUB2API_TEST_USAGE_FILTERS_DSN")
	if dsn == "" {
		t.Skip("isolated local PostgreSQL DSN required")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TEMP TABLE usage_logs (
 id BIGINT, user_id BIGINT, api_key_id BIGINT,account_id BIGINT,group_id BIGINT,request_id TEXT,model TEXT,requested_model TEXT,upstream_model TEXT,
 request_type SMALLINT,stream BOOLEAN,openai_ws_mode BOOLEAN,native_compaction_v2 BOOLEAN,billing_type SMALLINT,billing_mode TEXT,upstream_model_mismatch BOOLEAN,
 created_at TIMESTAMPTZ,input_tokens INT,output_tokens INT,cache_creation_tokens INT,cache_read_tokens INT,total_cost NUMERIC,actual_cost NUMERIC,account_stats_cost NUMERIC,account_rate_multiplier NUMERIC,duration_ms INT,inbound_endpoint TEXT,upstream_endpoint TEXT,image_count INT);
 CREATE TEMP TABLE users(id BIGINT,email TEXT,username TEXT);INSERT INTO users VALUES(11,'fixture@example.invalid','fixture');
 CREATE TEMP TABLE api_keys(id BIGINT,name TEXT);INSERT INTO api_keys VALUES(12,'fixture-key');
 CREATE TEMP TABLE groups(id BIGINT,name TEXT);INSERT INTO groups VALUES(14,'fixture-group');
 INSERT INTO usage_logs SELECT i,11,12,13,14,'req-match','upstream-model','client-alias','upstream-model',0,true,true,true,1,'image',false,'2026-10-01T12:00:00Z',10,20,30,40,7,3,NULL,1,100,'/v1/responses','/v1/responses',1 FROM generate_series(1,14) i;
 UPDATE usage_logs SET user_id=99 WHERE id=2;
 UPDATE usage_logs SET api_key_id=99 WHERE id=3;
 UPDATE usage_logs SET account_id=99 WHERE id=4;
 UPDATE usage_logs SET group_id=99 WHERE id=5;
 UPDATE usage_logs SET request_id='req-other' WHERE id=6;
 UPDATE usage_logs SET requested_model='other-alias' WHERE id=7;
 UPDATE usage_logs SET request_type=1,stream=false,openai_ws_mode=false WHERE id=8;
 UPDATE usage_logs SET native_compaction_v2=false WHERE id=9;
 UPDATE usage_logs SET billing_type=0 WHERE id=10;
 UPDATE usage_logs SET billing_mode='token' WHERE id=11;
 UPDATE usage_logs SET upstream_model_mismatch=true WHERE id=12;
 UPDATE usage_logs SET created_at='2026-09-30T23:59:59Z' WHERE id=13;
 UPDATE usage_logs SET created_at='2026-10-02T00:00:00Z' WHERE id=14;
 UPDATE usage_logs SET billing_mode=NULL WHERE id=1;`)
	require.NoError(t, err)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	kind := int16(service.RequestTypeWSV2)
	stream := false
	native := true
	bill := int8(1)
	audit := false
	f := usagestats.UsageLogFilters{UserID: 11, APIKeyID: 12, AccountID: 13, GroupID: 14, RequestID: "req-match", Model: "client-alias", ModelFilterSource: usagestats.ModelSourceRequested, RequestType: &kind, Stream: &stream, NativeCompactionV2: &native, BillingType: &bill, BillingMode: "image", UpstreamModelMismatch: &audit, StartTime: &start, EndTime: &end}
	repo := &usageLogRepository{sql: db}
	// Use the exact canonical WHERE builder used by ListWithFilters, without its association hydration.
	conditions, recordArgs := usageLogReadConditions(f)
	var records int64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs "+buildWhere(conditions), recordArgs...).Scan(&records))
	require.Equal(t, int64(1), records)

	stats, err := repo.GetStatsWithFilters(ctx, f)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, 7.0, stats.TotalCost)
	require.Equal(t, 3.0, stats.TotalActualCost)
	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "hour", f)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.Equal(t, int64(1), trend[0].Requests)
	models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, f, usagestats.ModelSourceUpstream)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "upstream-model", models[0].Model)
	require.Equal(t, int64(1), models[0].Requests)
	groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, f)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, int64(1), groups[0].Requests)
	keys, err := repo.GetAPIKeyUsageTrendWithUsageFilters(ctx, start, end, "hour", 12, f)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, int64(1), keys[0].Requests)
	users, err := repo.GetUserUsageTrendWithUsageFilters(ctx, start, end, "hour", 12, "actual_cost", f)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, int64(1), users[0].Requests)
	rank, err := repo.GetUserSpendingRankingWithUsageFilters(ctx, start, end, 12, f)
	require.NoError(t, err)
	require.Len(t, rank.Ranking, 1)
	require.Equal(t, int64(1), rank.TotalRequests)
	require.Equal(t, 3.0, rank.TotalActualCost)
	breakdown, err := repo.GetUserBreakdownStats(ctx, start, end, usagestats.UserBreakdownDimension{UserID: 11, APIKeyID: 12, AccountID: 13, GroupID: 14, Model: "client-alias", ModelType: usagestats.ModelSourceRequested, RequestID: "req-match", RequestType: &kind, Stream: &stream, NativeCompactionV2: &native, BillingType: &bill, BillingMode: "image", UpstreamModelMismatch: &audit}, 50)
	require.NoError(t, err)
	require.Len(t, breakdown, 1)
	require.Equal(t, int64(1), breakdown[0].Requests)
}
