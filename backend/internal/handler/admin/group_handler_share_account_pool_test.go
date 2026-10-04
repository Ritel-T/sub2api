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
	"testing"
)

type sharePoolHandlerAdmin struct {
	service.AdminService
	calls int
}

func (s *sharePoolHandlerAdmin) ShareAccountPool(context.Context, int64, service.ShareAccountPoolInput) (service.ShareAccountPoolResult, error) {
	s.calls++
	return service.ShareAccountPoolResult{Applied: true, AddedAccountIDs: []int64{2}, TotalAccounts: 2}, nil
}
func TestShareAccountPoolHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		code   int
	}{
		{"valid", func(map[string]any) {}, 200},
		{"missing target set", func(m map[string]any) { delete(m, "expected_target_account_ids") }, 400},
		{"nil source set", func(m map[string]any) { m["expected_source_account_ids"] = nil }, 400},
		{"unexpected setting", func(m map[string]any) { m["rate_multiplier"] = .2 }, 400},
		{"outside source", func(m map[string]any) { m["expected_target_account_ids"] = []int64{3} }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"source_group_id": 1, "expected_source_account_ids": []int64{1, 2}, "expected_target_account_ids": []int64{1}}
			tc.mutate(body)
			raw, e := json.Marshal(body)
			require.NoError(t, e)
			admin := &sharePoolHandlerAdmin{}
			h := &GroupHandler{adminService: admin}
			r := gin.New()
			r.POST("/groups/:id/share-account-pool", h.ShareAccountPool)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/groups/2/share-account-pool", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			if tc.code == 200 {
				require.Equal(t, 1, admin.calls)
			} else {
				require.Zero(t, admin.calls)
			}
		})
	}
}
