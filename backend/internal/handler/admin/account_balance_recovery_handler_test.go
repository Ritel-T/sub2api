package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type balanceRecoveryAdminStub struct {
	service.AdminService
	service.ScopedAdminAccounts
	calls int
	ids   []int64
}

func (s *balanceRecoveryAdminStub) PreviewAccountBalanceRecovery(_ context.Context, id int64) (*service.AccountBalanceRecoveryPreview, error) {
	return &service.AccountBalanceRecoveryPreview{AccountID: id, Eligible: true, Status: service.StatusError, Schedulable: false}, nil
}
func (s *balanceRecoveryAdminStub) RecoverAccountBalance(_ context.Context, id int64, req service.AccountBalanceRecoveryRequest) (*service.AccountBalanceRecoveryResult, error) {
	s.calls++
	s.ids = append(s.ids, id)
	return &service.AccountBalanceRecoveryResult{AccountID: id, Status: service.StatusActive, Schedulable: *req.TargetSchedulable, IndependentLimitsPreserved: true}, nil
}
func balanceRecoveryRouter(t *testing.T, s *balanceRecoveryAdminStub) *gin.Engine {
	t.Helper()
	old := service.DefaultIdempotencyCoordinator()
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(old) })
	service.SetDefaultIdempotencyCoordinator(nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &AccountHandler{adminService: s}
	r.GET("/api/v1/admin/accounts/:id/balance-recovery-preview", h.PreviewBalanceRecovery)
	r.POST("/api/v1/admin/accounts/:id/balance-recover", h.RecoverBalance)
	return r
}
func balanceRecoveryHandlerBody(t *testing.T) []byte {
	t.Helper()
	target := false
	b, err := json.Marshal(service.AccountBalanceRecoveryRequest{ExpectedUpdatedAt: time.Now().UTC(), ExpectedFingerprint: strings.Repeat("a", 64), TargetSchedulable: &target, FundingConfirmed: true, Confirm: true})
	require.NoError(t, err)
	return b
}
func balanceRecoveryPOST(r *gin.Engine, path, key string, body []byte) *httptest.ResponseRecorder {
	q := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	q.Header.Set("Content-Type", "application/json")
	q.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func TestBalanceRecoveryHandlerRequiresKeyStoreAndExplicitTarget(t *testing.T) {
	s := &balanceRecoveryAdminStub{}
	r := balanceRecoveryRouter(t, s)
	body := balanceRecoveryHandlerBody(t)
	require.Equal(t, http.StatusBadRequest, balanceRecoveryPOST(r, "/api/v1/admin/accounts/194/balance-recover", "", body).Code)
	require.Equal(t, http.StatusServiceUnavailable, balanceRecoveryPOST(r, "/api/v1/admin/accounts/194/balance-recover", "one", body).Code)
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	require.Equal(t, http.StatusBadRequest, balanceRecoveryPOST(r, "/api/v1/admin/accounts/194/balance-recover", "two", []byte(`{"confirm":true}`)).Code)
	require.Zero(t, s.calls)
}
func TestBalanceRecoveryHandlerReplaysAndBindsAccountAndTarget(t *testing.T) {
	s := &balanceRecoveryAdminStub{}
	r := balanceRecoveryRouter(t, s)
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	body := balanceRecoveryHandlerBody(t)
	first := balanceRecoveryPOST(r, "/api/v1/admin/accounts/194/balance-recover", "same", body)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	replay := balanceRecoveryPOST(r, "/api/v1/admin/accounts/194/balance-recover", "same", body)
	require.Equal(t, http.StatusOK, replay.Code)
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, s.calls)
	require.Equal(t, http.StatusConflict, balanceRecoveryPOST(r, "/api/v1/admin/accounts/195/balance-recover", "same", body).Code)
	require.Equal(t, 1, s.calls)
	changed := bytes.Replace(body, []byte(`"target_schedulable":false`), []byte(`"target_schedulable":true`), 1)
	require.Equal(t, http.StatusConflict, balanceRecoveryPOST(r, "/api/v1/admin/accounts/194/balance-recover", "same", changed).Code)
	require.Equal(t, 1, s.calls)
}

func TestBalanceRecoveryHandlerScopedMiddlewareForwardsNarrowEndpoints(t *testing.T) {
	svc := &balanceRecoveryAdminStub{}
	balanceRecoveryRouter(t, svc)
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	h := &AccountHandler{adminService: svc}
	scoped := gin.New()
	scoped.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), &adminaccess.Principal{}))
	})
	scoped.Use(h.ScopedAdminKeyAccounts)
	scoped.GET("/api/v1/admin/accounts/:id/balance-recovery-preview", func(c *gin.Context) { t.Fatal("must use safe scoped forwarding") })
	scoped.POST("/api/v1/admin/accounts/:id/balance-recover", func(c *gin.Context) { t.Fatal("must use scoped forwarding") })
	w := httptest.NewRecorder()
	scoped.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/194/balance-recovery-preview", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = balanceRecoveryPOST(scoped, "/api/v1/admin/accounts/194/balance-recover", "scoped-forward", balanceRecoveryHandlerBody(t))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, svc.calls)
}
