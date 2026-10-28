//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type resetHandlerUserRepo struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *resetHandlerUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	user := r.users[id]
	if user == nil {
		return nil, service.ErrUserNotFound
	}
	copy := *user
	return &copy, nil
}

type resetHandlerRepo struct {
	service.AccountRecoveryCodeRepository
	called int
	target int64
	epoch  string
	err    error
}

func (r *resetHandlerRepo) ResetAccountSecurity(_ context.Context, id int64, epoch string) error {
	r.called++
	r.target = id
	r.epoch = epoch
	return r.err
}

type resetHandlerCache struct {
	service.RefreshTokenCache
	err     error
	cleaned int64
}

func (c *resetHandlerCache) DeleteUserRefreshTokens(_ context.Context, id int64) error {
	c.cleaned = id
	return c.err
}

type resetHandlerFixture struct {
	h                *AuthHandler
	auth             *service.AuthService
	cfg              *config.Config
	repo             *resetHandlerRepo
	cache            *resetHandlerCache
	operator, target *service.User
	auditActor       any
	auditAction      any
	auditExtra       any
	auditService     *service.AuditLogService
}

func newResetHandlerFixture(t *testing.T) *resetHandlerFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "synthetic-reset-handler-signing-key", ExpireHour: 1}, AccountCenter: config.AccountCenterConfig{Enabled: true}}
	operator := &service.User{ID: 42, Email: "admin@example.test", Role: service.RoleAdmin, Status: service.StatusActive, PasswordHash: "synthetic"}
	target := &service.User{ID: 77, Email: "target@example.test", Role: service.RoleUser, Status: service.StatusActive, PasswordHash: "preserved"}
	users := &resetHandlerUserRepo{users: map[int64]*service.User{42: operator, 77: target}}
	settings := &userHandlerEpochRepo{values: map[string]string{}}
	cache := &resetHandlerCache{}
	auth := service.NewAuthService(nil, users, nil, cache, cfg, service.NewSettingService(settings, cfg), nil, nil, nil, nil, nil, nil, nil)
	repo := &resetHandlerRepo{}
	auth.SetAccountRecoveryCodeRepository(repo)
	return &resetHandlerFixture{h: &AuthHandler{cfg: cfg, authService: auth}, auth: auth, cfg: cfg, repo: repo, cache: cache, operator: operator, target: target}
}
func (f *resetHandlerFixture) token(t *testing.T, user *service.User, methods []string, age time.Duration) string {
	t.Helper()
	token, err := f.auth.GenerateToken(service.WithNativeAuthenticationProof(context.Background(), service.NativeAuthenticationProof{Methods: methods, AuthTime: time.Now().Add(-age).Unix()}), user)
	require.NoError(t, err)
	return token
}
func (f *resetHandlerFixture) request(id, body string, subjectID int64, headers ...string) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if subjectID > 0 {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: subjectID})
			c.Set(middleware2.ContextKeyAuthEmail, "admin@example.test")
		}
		c.Next()
		f.auditActor, _ = c.Get("audit_actor_id")
		f.auditAction, _ = c.Get("audit_action")
		f.auditExtra, _ = c.Get("audit_extra")
	})
	if f.auditService != nil {
		r.Use(gin.HandlerFunc(middleware2.NewAuditLogMiddleware(f.auditService)))
	}
	r.POST("/api/v1/admin/users/:id/account-security/reset", f.h.AccountSecurityAdminReset)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+id+"/account-security/reset", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, header := range headers {
		req.Header.Add("Authorization", header)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestAccountSecurityResetHandlerKnownSuccessAndAudit(t *testing.T) {
	f := newResetHandlerFixture(t)
	f.operator.TotpEnabled = true
	token := f.token(t, f.operator, []string{"pwd", "otp"}, 0)
	w := f.request("77", `{"confirmed":true,"verification_note":"Manually verified ownership"}`, 42, "Bearer "+token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"applied":true`)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, 1, f.repo.called)
	require.Equal(t, int64(77), f.repo.target)
	require.Len(t, f.repo.epoch, 32)
	require.Equal(t, int64(77), f.cache.cleaned)
	require.Equal(t, int64(42), f.auditActor)
	require.Equal(t, "admin.users.account_security.reset", f.auditAction)
	extra, ok := f.auditExtra.(map[string]any)
	require.True(t, ok)
	require.Equal(t, int64(77), extra["target_user_id"])
	require.Equal(t, "applied", extra["result"])
	for _, secret := range []string{"synthetic", "preserved", "admin@example.test", "Manually verified", f.repo.epoch} {
		require.NotContains(t, w.Body.String(), secret)
	}
}
func TestAccountSecurityResetHandlerRejectsNoncanonicalIDAndBodies(t *testing.T) {
	for _, id := range []string{"0", "-1", "+77", "077", "7e1", "9223372036854775808"} {
		t.Run("id_"+id, func(t *testing.T) {
			f := newResetHandlerFixture(t)
			token := f.token(t, f.operator, []string{"pwd"}, 0)
			w := f.request(id, `{"confirmed":true,"verification_note":"Verified"}`, 42, "Bearer "+token)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Zero(t, f.repo.called)
		})
	}
	for _, body := range []string{
		`{}`, `null`, `{"confirmed":false,"verification_note":"Verified"}`, `{"confirmed":true,"verification_note":" "}`,
		`{"confirmed":"true","verification_note":"Verified"}`, `{"confirmed":true,"verification_note":"Verified","operator_id":42}`,
		`{"confirmed":true,"verification_note":"Verified","password":"private"}`, `{"confirmed":true,"verification_note":"Verified","recovery_code":"private"}`,
		`{"confirmed":true,"verification_note":"Verified"} {}`, "{", strings.Repeat("x", 8193),
		`{"confirmed":true,"verification_note":"` + strings.Repeat("x", 1001) + `"}`,
	} {
		t.Run("body", func(t *testing.T) {
			f := newResetHandlerFixture(t)
			token := f.token(t, f.operator, []string{"pwd"}, 0)
			w := f.request("77", body, 42, "Bearer "+token)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Zero(t, f.repo.called)
			require.NotContains(t, w.Body.String(), "private")
		})
	}
}
func TestAccountSecurityResetHandlerRequiresNativeMiddlewareIdentity(t *testing.T) {
	for _, mode := range []string{"no_subject", "no_bearer", "admin_key", "duplicate_bearer", "wrong_user", "forged", "stale", "recovery_without_password", "non_admin", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			f := newResetHandlerFixture(t)
			f.operator.TotpEnabled = true
			token := f.token(t, f.operator, []string{"pwd", "otp"}, 0)
			subject := int64(42)
			headers := []string{"Bearer " + token}
			switch mode {
			case "no_subject":
				subject = 0
			case "no_bearer":
				headers = nil
			case "admin_key":
				headers = []string{"Bearer fake-admin-key"}
			case "duplicate_bearer":
				headers = append(headers, headers[0])
			case "wrong_user":
				headers = []string{"Bearer " + f.token(t, f.target, []string{"pwd"}, 0)}
			case "forged":
				headers = []string{"Bearer " + token + "x"}
			case "stale":
				headers = []string{"Bearer " + f.token(t, f.operator, []string{"pwd", "otp"}, 30*time.Minute+time.Second)}
			case "recovery_without_password":
				headers = []string{"Bearer " + f.token(t, f.operator, []string{"recovery"}, 0)}
			case "non_admin":
				f.operator.Role = service.RoleUser
			case "disabled":
				f.cfg.AccountCenter.Enabled = false
			}
			w := f.request("77", `{"confirmed":true,"verification_note":"Verified"}`, subject, headers...)
			require.GreaterOrEqual(t, w.Code, 400, w.Body.String())
			require.Zero(t, f.repo.called)
			require.NotContains(t, w.Body.String(), `"applied":true`)
		})
	}
}
func TestAccountSecurityResetHandlerThirtyMinuteProofBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		age     time.Duration
		allowed bool
	}{
		{"past_legacy_five_minute_window", 6 * time.Minute, true},
		{"one_second_before_deadline", 30*time.Minute - time.Second, true},
		{"inclusive_deadline", 30 * time.Minute, true},
		{"one_second_after_deadline", 30*time.Minute + time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newResetHandlerFixture(t)
				f.operator.TotpEnabled = true
				token := f.token(t, f.operator, []string{"pwd", "otp"}, tc.age)
				w := f.request("77", `{"confirmed":true,"verification_note":"Verified"}`, 42, "Bearer "+token)
				if tc.allowed {
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.Equal(t, 1, f.repo.called)
					require.Equal(t, int64(77), f.cache.cleaned)
				} else {
					require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), "REAUTH_REQUIRED")
					require.Zero(t, f.repo.called)
					require.Zero(t, f.cache.cleaned)
					require.NotContains(t, w.Body.String(), `"applied":true`)
				}
			})
		})
	}
}

func TestAccountSecurityResetHandlerUnknownNeverReportsApplied(t *testing.T) {
	for _, mode := range []string{"redis", "commit", "storage", "missing_user"} {
		t.Run(mode, func(t *testing.T) {
			f := newResetHandlerFixture(t)
			token := f.token(t, f.operator, []string{"pwd"}, 0)
			switch mode {
			case "redis":
				f.cache.err = errors.New("private cache material")
			case "commit":
				f.repo.err = service.ErrAccountSecurityResetUnknown
			case "storage":
				f.repo.err = errors.New("private SQL material")
			case "missing_user":
				f.repo.err = service.ErrUserNotFound
			}
			w := f.request("77", `{"confirmed":true,"verification_note":"Verified"}`, 42, "Bearer "+token)
			require.GreaterOrEqual(t, w.Code, 400)
			require.NotContains(t, w.Body.String(), `"applied":true`)
			require.NotContains(t, w.Body.String(), "private")
			if mode == "redis" || mode == "commit" {
				require.Contains(t, w.Body.String(), "ACCOUNT_SECURITY_RESET_OUTCOME_UNKNOWN")
				extra, ok := f.auditExtra.(map[string]any)
				require.True(t, ok)
				require.Equal(t, "unknown", extra["result"])
			}
			if mode != "redis" {
				require.Zero(t, f.cache.cleaned)
			}
		})
	}
}

type resetHandlerAuditRepo struct {
	service.AuditLogRepository
	mu   sync.Mutex
	logs []*service.AuditLog
}

func (r *resetHandlerAuditRepo) BatchInsert(_ context.Context, logs []*service.AuditLog) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, logs...)
	return int64(len(logs)), nil
}
func TestAccountSecurityResetHandlerOriginalRequestAudit(t *testing.T) {
	f := newResetHandlerFixture(t)
	auditRepo := &resetHandlerAuditRepo{}
	audit := service.NewAuditLogService(auditRepo, nil)
	audit.StartWriter()
	defer audit.Stop()
	f.auditService = audit
	token := f.token(t, f.operator, []string{"pwd"}, 0)
	w := f.request("77", `{"confirmed":true,"verification_note":"Manually verified with administrator"}`, 42, "Bearer "+token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	audit.Stop()
	auditRepo.mu.Lock()
	defer auditRepo.mu.Unlock()
	require.Len(t, auditRepo.logs, 1)
	entry := auditRepo.logs[0]
	require.NotNil(t, entry.ActorUserID)
	require.Equal(t, int64(42), *entry.ActorUserID)
	require.Equal(t, "admin.users.account_security.reset", entry.Action)
	require.Equal(t, "/api/v1/admin/users/:id/account-security/reset", entry.Path)
	// Verify safe audit summaries independently of the root's body-omission policy.
	require.Equal(t, int64(77), entry.Extra["target_user_id"])
	require.Equal(t, true, entry.Extra["confirm"])
	require.Equal(t, "applied", entry.Extra["result"])
	require.NotContains(t, entry.RequestBody, "synthetic")
	require.NotContains(t, entry.RequestBody, f.repo.epoch)
}

func TestAccountSecurityResetHandlerBackupRecoveryFactor(t *testing.T) {
	f := newResetHandlerFixture(t)
	f.operator.TotpEnabled = true
	token := f.token(t, f.operator, []string{"pwd", "recovery"}, 0)
	w := f.request("77", `{"confirmed":true,"verification_note":"Manually verified identity"}`, 42, "Bearer "+token)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"applied":true`)
	require.Equal(t, 1, f.repo.called)
	require.Equal(t, int64(77), f.repo.target)
	require.Equal(t, int64(77), f.cache.cleaned)
}
