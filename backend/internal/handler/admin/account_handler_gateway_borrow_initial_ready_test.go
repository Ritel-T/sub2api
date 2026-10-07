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

type initialReadyHandlerRepo struct {
	service.AccountRepository
	calls int
}

func (r *initialReadyHandlerRepo) MarkGatewayBorrowInitialReadyIfObserved(context.Context, int64, service.GatewayBorrowInitialReadyObservation) (service.GatewayBorrowPolicyResult, error) {
	r.calls++
	return service.GatewayBorrowPolicyResult{Applied: true, Reason: "initial_defaults_ready"}, nil
}
func TestGatewayBorrowInitialReadyHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		code   int
	}{
		{"valid", func(map[string]any) {}, 200},
		{"missing proxy", func(b map[string]any) { delete(b, "expected_proxy_id") }, 400},
		{"unknown", func(b map[string]any) { b["extra"] = map[string]any{} }, 400},
		{"bad config", func(b map[string]any) { b["expected_config"] = map[string]any{} }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "expected_proxy_id": nil, "credential_sha256": strings.Repeat("a", 64), "expected_policy": map[string]any{}, "expected_config": map[string]any{"concurrency": 3, "priority": 4, "load_factor": nil, "group_ids": []int64{}, "model_mapping": nil}}
			tc.change(body)
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			repo := &initialReadyHandlerRepo{}
			h := &AccountHandler{rateLimitService: service.NewRateLimitService(repo, nil, nil, nil, nil)}
			r := gin.New()
			r.POST("/accounts/:id/gateway-borrow-initial-ready", h.MarkGatewayBorrowInitialReady)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/accounts/1/gateway-borrow-initial-ready", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			if tc.code == 200 {
				require.Equal(t, 1, repo.calls)
				require.Contains(t, rec.Body.String(), "initial_defaults_ready")
			} else {
				require.Zero(t, repo.calls)
			}
		})
	}
}
