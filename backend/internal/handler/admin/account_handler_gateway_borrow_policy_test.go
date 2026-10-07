package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type gatewayBorrowHandlerRepo struct {
	service.AccountRepository
	calls    int
	observed service.GatewayBorrowPolicyObservation
}

func (r *gatewayBorrowHandlerRepo) UpdateGatewayBorrowPolicyIfObserved(_ context.Context, _ int64, o service.GatewayBorrowPolicyObservation) (service.GatewayBorrowPolicyResult, error) {
	r.calls++
	r.observed = o
	return service.GatewayBorrowPolicyResult{Applied: true, Reason: "applied"}, nil
}
func TestGatewayBorrowPolicyHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		code   int
	}{
		{"valid retire only", func(map[string]any) {}, 200},
		{"account mode reusable classification", func(b map[string]any) {
			b["policy_mode"] = service.GatewayBorrowAccountQualityMode
			b["retire_bps"] = false
			b["model_results"] = map[string]any{}
		}, 200},
		{"unknown account mode", func(b map[string]any) { b["policy_mode"] = "unknown" }, 400},
		{"missing proxy", func(b map[string]any) { delete(b, "expected_proxy_id") }, 400},
		{"null borrow models", func(b map[string]any) { b["borrow_models"] = nil }, 400},
		{"unknown extra write", func(b map[string]any) { b["expected_policy"] = map[string]any{"codex_7d_used_percent": 0} }, 400},
		{"identity malformed", func(b map[string]any) { b["credential_sha256"] = "token" }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "expected_proxy_id": nil, "credential_sha256": strings.Repeat("a", 64), "expected_policy": map[string]any{}, "borrow_models": []string{}, "retire_bps": true}
			tc.change(body)
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			repo := &gatewayBorrowHandlerRepo{}
			svc := service.NewRateLimitService(repo, nil, nil, nil, nil)
			h := &AccountHandler{rateLimitService: svc}
			rec := httptest.NewRecorder()
			r := gin.New()
			r.POST("/accounts/:id/gateway-borrow-policy", h.UpdateGatewayBorrowPolicy)
			req := httptest.NewRequest(http.MethodPost, "/accounts/1/gateway-borrow-policy", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			if tc.code == 200 {
				require.Equal(t, 1, repo.calls)
				require.Contains(t, rec.Body.String(), "\"applied\":true")
				require.Nil(t, repo.observed.ExpectedProxyID)
			} else {
				require.Zero(t, repo.calls)
			}
		})
	}
}
