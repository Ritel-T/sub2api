package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexProbeUsageHandlerRepo struct {
	service.AccountRepository
	gets, calls int
	account     *service.Account
	updated     bool
	observed    service.CodexProbeUsageObservation
	previous    *string
	quota       map[string]any
}

func (r *codexProbeUsageHandlerRepo) GetByID(context.Context, int64) (*service.Account, error) {
	r.gets++
	return r.account, nil
}
func (r *codexProbeUsageHandlerRepo) UpdateCodexUsageSnapshotIfObserved(_ context.Context, _ int64, observed service.CodexProbeUsageObservation, previous *string, updates map[string]any) (bool, error) {
	r.calls++
	r.observed, r.previous, r.quota = observed, previous, updates
	return r.updated, nil
}

func TestCodexProbeUsageSnapshotHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Truncate(time.Second)
	validHeaders := map[string]string{
		"x-codex-primary-used-percent":                 "12.5",
		"x-codex-primary-reset-after-seconds":          "86400",
		"x-codex-primary-window-minutes":               "10080",
		"x-codex-secondary-used-percent":               "1.25",
		"x-codex-secondary-reset-after-seconds":        "1800",
		"x-codex-secondary-window-minutes":             "300",
		"x-codex-primary-over-secondary-limit-percent": "200",
	}
	for _, tc := range []struct {
		name               string
		mutate             func(map[string]any, *codexProbeUsageHandlerRepo)
		code, gets, writes int
		updated            bool
	}{
		{name: "fresh normal snapshot", code: 200, gets: 1, writes: 1, updated: true},
		{name: "atomic compare loses race", code: 200, gets: 1, writes: 1},
		{name: "explicit null proxy", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { body["observed_proxy_id"] = nil }, code: 200, gets: 1, writes: 1, updated: true},
		{name: "headers case insensitive", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"X-Codex-Secondary-Used-Percent": "0"}
		}, code: 200, gets: 1, writes: 1, updated: true},
		{name: "stale observation", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["observed_at"] = now.Add(-11 * time.Minute).Format(time.RFC3339)
		}, code: 200},
		{name: "future observation", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["observed_at"] = now.Add(time.Minute).Format(time.RFC3339)
		}, code: 400},
		{name: "missing observation", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { delete(body, "observed_at") }, code: 400},
		{name: "non UTC observation", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["observed_at"] = "2026-10-03T11:00:00+01:00"
		}, code: 400},
		{name: "missing proxy", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { delete(body, "observed_proxy_id") }, code: 400},
		{name: "negative proxy", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { body["observed_proxy_id"] = -1 }, code: 400},
		{name: "string proxy", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { body["observed_proxy_id"] = "25" }, code: 400},
		{name: "missing token hash", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { delete(body, "observed_access_token_sha256") }, code: 400},
		{name: "invalid token hash", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["observed_access_token_sha256"] = "raw-token"
		}, code: 400},
		{name: "no headers", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) { body["headers"] = map[string]string{} }, code: 400},
		{name: "arbitrary extra write", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"bps_enabled": "false"}
		}, code: 400},
		{name: "duplicate case header", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-used-percent": "5", "X-Codex-Primary-Used-Percent": "6"}
		}, code: 400},
		{name: "NaN", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-used-percent": "NaN"}
		}, code: 400},
		{name: "infinity", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-secondary-used-percent": "+Inf"}
		}, code: 400},
		{name: "negative percent", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-used-percent": "-1"}
		}, code: 400},
		{name: "over 100 percent", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-secondary-used-percent": "101"}
		}, code: 400},
		{name: "negative ratio", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-over-secondary-limit-percent": "-1"}
		}, code: 400},
		{name: "negative reset", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-reset-after-seconds": "-1"}
		}, code: 400},
		{name: "reset duration overflow", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-secondary-reset-after-seconds": "9223372036854775807"}
		}, code: 400},
		{name: "zero inactive window", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-used-percent": "1", "x-codex-primary-window-minutes": "10080", "x-codex-secondary-used-percent": "0", "x-codex-secondary-window-minutes": "0", "x-codex-secondary-reset-after-seconds": "0"}
		}, code: 200, gets: 1, writes: 1, updated: true},
		{name: "negative window", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-window-minutes": "-1"}
		}, code: 400},
		{name: "non integer window", mutate: func(body map[string]any, _ *codexProbeUsageHandlerRepo) {
			body["headers"] = map[string]string{"x-codex-primary-window-minutes": "300.5"}
		}, code: 400},
		{name: "newer existing snapshot", mutate: func(_ map[string]any, r *codexProbeUsageHandlerRepo) {
			r.account.Extra["codex_usage_updated_at"] = now.Add(time.Second).Format(time.RFC3339)
		}, code: 200, gets: 1},
		{name: "equal snapshot is idempotent", mutate: func(_ map[string]any, r *codexProbeUsageHandlerRepo) {
			r.account.Extra["codex_usage_updated_at"] = now.Format(time.RFC3339)
		}, code: 200, gets: 1},
		{name: "malformed prior timestamp fails closed", mutate: func(_ map[string]any, r *codexProbeUsageHandlerRepo) {
			r.account.Extra["codex_usage_updated_at"] = "invalid"
		}, code: 200, gets: 1},
		{name: "API key account", mutate: func(_ map[string]any, r *codexProbeUsageHandlerRepo) { r.account.Type = service.AccountTypeAPIKey }, code: 200, gets: 1},
		{name: "shadow account", mutate: func(_ map[string]any, r *codexProbeUsageHandlerRepo) {
			id := int64(123)
			r.account.ParentAccountID = &id
		}, code: 200, gets: 1},
		{name: "other platform", mutate: func(_ map[string]any, r *codexProbeUsageHandlerRepo) { r.account.Platform = service.PlatformGrok }, code: 200, gets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := now.Add(-time.Hour).Format(time.RFC3339)
			repo := &codexProbeUsageHandlerRepo{updated: tc.updated, account: &service.Account{
				ID: 113, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "observed-token"},
				Extra:       map[string]any{"codex_usage_updated_at": previous, "bps_enabled": true},
			}}
			body := map[string]any{
				"observed_at": now.Format(time.RFC3339), "headers": validHeaders, "observed_proxy_id": 25,
				"observed_access_token_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte("observed-token"))),
			}
			if tc.mutate != nil {
				tc.mutate(body, repo)
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			h := &AccountHandler{rateLimitService: service.NewRateLimitService(repo, nil, nil, nil, nil)}
			router := gin.New()
			router.POST("/accounts/:id/codex-usage-snapshot", h.UpdateCodexUsageSnapshot)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/accounts/113/codex-usage-snapshot", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			require.Equal(t, tc.gets, repo.gets)
			require.Equal(t, tc.writes, repo.calls)
			if tc.code == 200 {
				var reply struct {
					Data struct {
						Updated bool           `json:"updated"`
						Quota   map[string]any `json:"quota"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reply))
				require.Equal(t, tc.updated, reply.Data.Updated)
				if tc.name == "zero inactive window" {
					require.Equal(t, float64(0), reply.Data.Quota["codex_secondary_window_minutes"])
					require.Equal(t, float64(0), reply.Data.Quota["codex_5h_used_percent"])
					require.Equal(t, float64(0), reply.Data.Quota["codex_5h_window_minutes"])
					require.Equal(t, now.Format(time.RFC3339), reply.Data.Quota["codex_5h_reset_at"])
					require.Equal(t, float64(1), reply.Data.Quota["codex_7d_used_percent"])
					require.Equal(t, float64(10080), reply.Data.Quota["codex_7d_window_minutes"])
				}
				if tc.name == "fresh normal snapshot" {
					require.Equal(t, previous, *repo.previous)
					require.Equal(t, 1.25, reply.Data.Quota["codex_5h_used_percent"])
					require.Equal(t, 12.5, reply.Data.Quota["codex_7d_used_percent"])
					require.Equal(t, now.Add(1800*time.Second).Format(time.RFC3339), reply.Data.Quota["codex_5h_reset_at"])
					require.Equal(t, now.Add(86400*time.Second).Format(time.RFC3339), reply.Data.Quota["codex_7d_reset_at"])
					require.Equal(t, now.Format(time.RFC3339), reply.Data.Quota["codex_usage_updated_at"])
					require.NotContains(t, reply.Data.Quota, "bps_enabled")
				}
			}
		})
	}
}
