package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeRateLimitHandlerRepo struct {
	service.AccountRepository
	calls    int
	id       int64
	observed service.NativeRateLimitClearObservation
	cleared  bool
}

func (r *nativeRateLimitHandlerRepo) ClearNativeRateLimitIfObserved(_ context.Context, id int64, observed service.NativeRateLimitClearObservation) (bool, error) {
	r.calls++
	r.id, r.observed = id, observed
	return r.cleared, nil
}

func TestClearNativeRateLimitHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, id, body string
		code           int
		calls          int
		cleared        bool
	}{
		{"match", "113", `{"observed_rate_limited_at":"2026-10-02T16:00:00Z","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":25}`, 200, 1, true},
		{"mismatch no-op", "113", `{"observed_rate_limited_at":"2026-10-02T16:00:00Z","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":25}`, 200, 1, false},
		{"no proxy", "113", `{"observed_rate_limited_at":"2026-10-02T16:00:00Z","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":null}`, 200, 1, true},
		{"missing proxy observation", "113", `{"observed_rate_limited_at":"2026-10-02T16:00:00Z","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z"}`, 400, 0, false},
		{"missing generation", "113", `{"observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":25}`, 400, 0, false},
		{"invalid generation", "113", `{"observed_rate_limited_at":"invalid","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":25}`, 400, 0, false},
		{"invalid proxy", "113", `{"observed_rate_limited_at":"2026-10-02T16:00:00Z","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":"25"}`, 400, 0, false},
		{"invalid hash", "113", `{"observed_rate_limited_at":"2026-10-02T16:00:00Z","observed_rate_limit_reset_at":"2026-10-03T16:00:00Z","observed_proxy_id":25,"observed_access_token_sha256":"raw-token"}`, 400, 0, false},
		{"invalid ID", "0", `{}`, 400, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &nativeRateLimitHandlerRepo{cleared: tc.cleared}
			h := &AccountHandler{rateLimitService: service.NewRateLimitService(repo, nil, nil, nil, nil)}
			router := gin.New()
			router.POST("/accounts/:id/clear-native-rate-limit", h.ClearNativeRateLimit)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/accounts/"+tc.id+"/clear-native-rate-limit", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			require.Equal(t, tc.calls, repo.calls)
			if tc.calls == 1 {
				var result struct {
					Data struct {
						ID      int64 `json:"id"`
						Cleared bool  `json:"cleared"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				require.Equal(t, int64(113), result.Data.ID)
				require.Equal(t, tc.cleared, result.Data.Cleared)
				require.Equal(t, int64(113), repo.id)
				require.False(t, repo.observed.RateLimitedAt.IsZero())
				require.False(t, repo.observed.RateLimitResetAt.IsZero())
				if tc.name == "no proxy" {
					require.Nil(t, repo.observed.ProxyID)
				} else {
					require.Equal(t, int64(25), *repo.observed.ProxyID)
				}
			}
		})
	}
}
