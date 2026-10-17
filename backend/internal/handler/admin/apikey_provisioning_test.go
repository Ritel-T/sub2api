package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const provisioningTestPassword = "saved-provisioning-password"
const provisioningTestKey = "sk-provisioning-exact-0123456789"

type provisioningUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *provisioningUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	if r.user == nil || r.user.ID != id || r.user.DeletedAt != nil {
		return nil, service.ErrUserNotFound
	}
	return r.user, nil
}

type provisioningAdminService struct {
	service.AdminService
	users *provisioningUserRepo
}

func (s *provisioningAdminService) GetUser(ctx context.Context, id int64) (*service.User, error) {
	return s.users.GetByID(ctx, id)
}

type provisioningGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r *provisioningGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if r.group == nil || r.group.ID != id {
		return nil, service.ErrGroupNotFound
	}
	return r.group, nil
}

type provisioningSubscriptionRepo struct {
	service.UserSubscriptionRepository
	active bool
}

func (r *provisioningSubscriptionRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	if !r.active {
		return nil, service.ErrSubscriptionNotFound
	}
	return &service.UserSubscription{UserID: userID, GroupID: groupID}, nil
}

type provisioningKeyRepo struct {
	service.APIKeyRepository
	created []*service.APIKey
}

func (r *provisioningKeyRepo) ExistsByKey(_ context.Context, key string) (bool, error) {
	for _, created := range r.created {
		if created.Key == key {
			return true, nil
		}
	}
	return false, nil
}

func (r *provisioningKeyRepo) Create(_ context.Context, key *service.APIKey) error {
	key.ID = int64(len(r.created) + 71)
	key.CreatedAt = time.Now().UTC()
	key.UpdatedAt = key.CreatedAt
	r.created = append(r.created, key)
	return nil
}

type provisioningFixture struct {
	handler *AdminAPIKeyHandler
	users   *provisioningUserRepo
	groups  *provisioningGroupRepo
	subs    *provisioningSubscriptionRepo
	keys    *provisioningKeyRepo
}

func newProvisioningFixture(t *testing.T) *provisioningFixture {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(provisioningTestPassword), bcrypt.MinCost)
	require.NoError(t, err)
	users := &provisioningUserRepo{user: &service.User{
		ID: 17, Role: service.RoleUser, Status: service.StatusActive, PasswordHash: string(hash),
		AllowedGroups: []int64{2}, RestrictPublicGroups: true,
	}}
	groups := &provisioningGroupRepo{group: &service.Group{ID: 2, IsExclusive: true, Status: service.StatusActive}}
	subs := &provisioningSubscriptionRepo{}
	keys := &provisioningKeyRepo{}
	svc := service.NewAPIKeyService(keys, users, groups, subs, nil, nil, &config.Config{})
	f := &provisioningFixture{
		handler: NewAdminAPIKeyHandler(&provisioningAdminService{users: users}, svc),
		users:   users, groups: groups, subs: subs, keys: keys,
	}
	idemCfg := service.DefaultIdempotencyConfig()
	idemCfg.ObserveOnly = false
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), idemCfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	return f
}

func (f *provisioningFixture) router(role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if role != "" {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			c.Set(string(middleware.ContextKeyUserRole), role)
		})
	}
	router.POST("/api/v1/admin/users/:id/api-keys", f.handler.CreateForUser)
	router.POST("/api/v1/admin/users/:id/verify-password", f.handler.VerifyUserPassword)
	return router
}

func provisioningRequest(router *gin.Engine, path, body, idempotencyKey string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func provisioningCreateBody() string {
	return `{"name":"Telegram","group_id":2,"custom_key":"` + provisioningTestKey + `","expected_password":"` + provisioningTestPassword + `"}`
}

func TestAdminAPIKeyProvisioning_RequiresAdmin(t *testing.T) {
	for _, role := range []string{"", service.RoleUser, service.RoleObserver} {
		t.Run("role_"+role, func(t *testing.T) {
			f := newProvisioningFixture(t)
			for _, path := range []string{"17/api-keys", "17/verify-password"} {
				rec := provisioningRequest(f.router(role), path, provisioningCreateBody(), "operation-1")
				if role == "" {
					require.Equal(t, http.StatusUnauthorized, rec.Code)
				} else {
					require.Equal(t, http.StatusForbidden, rec.Code)
				}
			}
			require.Empty(t, f.keys.created)
		})
	}
}

func TestAdminAPIKeyProvisioning_InvalidTargetCannotWrite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     string
		change func(*provisioningFixture)
		status int
	}{
		{name: "invalid id", id: "abc", status: http.StatusBadRequest},
		{name: "zero id", id: "0", status: http.StatusBadRequest},
		{name: "negative id", id: "-1", status: http.StatusBadRequest},
		{name: "missing", id: "99", status: http.StatusNotFound},
		{name: "deleted", id: "17", status: http.StatusNotFound, change: func(f *provisioningFixture) { now := time.Now(); f.users.user.DeletedAt = &now }},
		{name: "admin", id: "17", status: http.StatusBadRequest, change: func(f *provisioningFixture) { f.users.user.Role = service.RoleAdmin }},
		{name: "observer", id: "17", status: http.StatusBadRequest, change: func(f *provisioningFixture) { f.users.user.Role = service.RoleObserver }},
		{name: "disabled", id: "17", status: http.StatusBadRequest, change: func(f *provisioningFixture) { f.users.user.Status = service.StatusDisabled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProvisioningFixture(t)
			if tc.change != nil {
				tc.change(f)
			}
			for _, path := range []string{tc.id + "/api-keys", tc.id + "/verify-password"} {
				body := provisioningCreateBody()
				if path == tc.id+"/verify-password" {
					body = `{"password":"` + provisioningTestPassword + `"}`
				}
				rec := provisioningRequest(f.router(service.RoleAdmin), path, body, "operation-1")
				require.Equal(t, tc.status, rec.Code, rec.Body.String())
			}
			require.Empty(t, f.keys.created)
		})
	}
}

func TestAdminAPIKeyProvisioning_ExactKeyAndIdempotency(t *testing.T) {
	f := newProvisioningFixture(t)
	router := f.router(service.RoleAdmin)
	body := provisioningCreateBody()
	originalHash := f.users.user.PasswordHash
	first := provisioningRequest(router, "17/api-keys", body, "operation-1")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var envelope struct {
		Data dto.APIKey `json:"data"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &envelope))
	require.Equal(t, int64(71), envelope.Data.ID)
	require.Equal(t, int64(17), envelope.Data.UserID)
	require.Equal(t, provisioningTestKey, envelope.Data.Key)
	require.NotNil(t, envelope.Data.GroupID)
	require.Equal(t, int64(2), *envelope.Data.GroupID)
	require.Equal(t, service.StatusActive, envelope.Data.Status)
	require.Equal(t, originalHash, f.users.user.PasswordHash)
	require.Equal(t, []int64{2}, f.users.user.AllowedGroups)
	require.Nil(t, f.users.user.LastLoginAt)

	replay := provisioningRequest(router, "17/api-keys", body, "operation-1")
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.Equal(t, "true", replay.Header().Get("X-Idempotency-Replayed"))
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.Len(t, f.keys.created, 1)

	changed := `{"name":"Different","group_id":2,"custom_key":"` + provisioningTestKey + `"}`
	conflict := provisioningRequest(router, "17/api-keys", changed, "operation-1")
	require.Equal(t, http.StatusConflict, conflict.Code)
	require.Contains(t, conflict.Body.String(), "IDEMPOTENCY_KEY_CONFLICT")
	require.Len(t, f.keys.created, 1)

	// A reused key must conflict across target IDs, even though FullPath is equal.
	f.users.user.ID = 18
	otherUser := provisioningRequest(router, "18/api-keys", body, "operation-1")
	require.Equal(t, http.StatusConflict, otherUser.Code)
	require.Len(t, f.keys.created, 1)
}

func TestAdminAPIKeyProvisioning_PreservesGroupAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*provisioningFixture)
		status int
	}{
		{"exclusive unauthorized", func(f *provisioningFixture) { f.users.user.AllowedGroups = nil }, http.StatusForbidden},
		{"restricted public unauthorized", func(f *provisioningFixture) { f.groups.group.IsExclusive = false; f.users.user.AllowedGroups = nil }, http.StatusForbidden},
		{"public authorized", func(f *provisioningFixture) {
			f.groups.group.IsExclusive = false
			f.users.user.AllowedGroups = nil
			f.users.user.RestrictPublicGroups = false
		}, http.StatusOK},
		{"subscription required", func(f *provisioningFixture) { f.groups.group.SubscriptionType = service.SubscriptionTypeSubscription }, http.StatusForbidden},
		{"subscription authorized", func(f *provisioningFixture) {
			f.groups.group.SubscriptionType = service.SubscriptionTypeSubscription
			f.subs.active = true
		}, http.StatusOK},
		{"missing group", func(f *provisioningFixture) { f.groups.group = nil }, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProvisioningFixture(t)
			tc.setup(f)
			before := append([]int64(nil), f.users.user.AllowedGroups...)
			rec := provisioningRequest(f.router(service.RoleAdmin), "17/api-keys", provisioningCreateBody(), "operation-1")
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Equal(t, before, f.users.user.AllowedGroups)
			if tc.status != http.StatusOK {
				require.Empty(t, f.keys.created)
			}
		})
	}
}

func TestAdminAPIKeyProvisioning_PasswordCheckNeverResets(t *testing.T) {
	f := newProvisioningFixture(t)
	router := f.router(service.RoleAdmin)
	originalHash := f.users.user.PasswordHash
	for _, password := range []string{provisioningTestPassword, "wrong-password"} {
		body := `{"password":"` + password + `"}`
		rec := provisioningRequest(router, "17/verify-password", body, "")
		require.Equal(t, http.StatusOK, rec.Code)
		var envelope struct {
			Data struct {
				UserID int64 `json:"user_id"`
				Valid  bool  `json:"valid"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
		require.Equal(t, int64(17), envelope.Data.UserID)
		require.Equal(t, password == provisioningTestPassword, envelope.Data.Valid)
		require.NotContains(t, rec.Body.String(), password)
		require.NotContains(t, rec.Body.String(), "token")
	}
	wrong := `{"name":"Telegram","group_id":2,"custom_key":"` + provisioningTestKey + `","expected_password":"wrong-password"}`
	rec := provisioningRequest(router, "17/api-keys", wrong, "operation-1")
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "USER_PASSWORD_MISMATCH")
	require.NotContains(t, rec.Body.String(), "wrong-password")
	require.Empty(t, f.keys.created)
	require.Equal(t, originalHash, f.users.user.PasswordHash)
	require.Nil(t, f.users.user.LastLoginAt)
	require.Nil(t, f.users.user.LastActiveAt)

	// Existing users may receive a first key without disclosing/changing a password.
	keyOnly := `{"name":"Telegram","group_id":2,"custom_key":"` + provisioningTestKey + `"}`
	rec = provisioningRequest(router, "17/api-keys", keyOnly, "operation-key-only")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.keys.created, 1)
	require.Equal(t, originalHash, f.users.user.PasswordHash)
}

func TestAdminAPIKeyProvisioning_RejectsInvalidInputWithoutSecrets(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unknown permission field", `{"name":"Telegram","allowed_groups":[2]}`},
		{"empty expected password", `{"name":"Telegram","expected_password":""}`},
		{"invalid group", `{"name":"Telegram","group_id":0}`},
		{"empty name", `{"name":""}`},
		{"trailing JSON", `{"name":"Telegram"} {"password":"secret-input"}`},
		{"wrong type", `{"name":"Telegram","expected_password":{"secret-input":true}}`},
		{"invalid custom key", `{"name":"Telegram","custom_key":"secret-input!"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProvisioningFixture(t)
			rec := provisioningRequest(f.router(service.RoleAdmin), "17/api-keys", tc.body, "operation-1")
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.NotContains(t, rec.Body.String(), "secret-input")
			require.Empty(t, f.keys.created)
		})
	}
}

func TestAdminAPIKeyProvisioning_RequiresIdempotencyWithoutStore(t *testing.T) {
	f := newProvisioningFixture(t)
	router := f.router(service.RoleAdmin)
	noKey := provisioningRequest(router, "17/api-keys", provisioningCreateBody(), "")
	require.Equal(t, http.StatusBadRequest, noKey.Code)
	require.Contains(t, noKey.Body.String(), "IDEMPOTENCY_KEY_REQUIRED")
	service.SetDefaultIdempotencyCoordinator(nil)
	noStore := provisioningRequest(router, "17/api-keys", provisioningCreateBody(), "operation-1")
	require.Equal(t, http.StatusServiceUnavailable, noStore.Code)
	require.Empty(t, f.keys.created)
}

func TestAdminAPIKeyProvisioning_AuditRedactsCredentials(t *testing.T) {
	redacted := service.RedactAuditBody([]byte(provisioningCreateBody()), "application/json")
	require.NotContains(t, redacted, provisioningTestPassword)
	require.NotContains(t, redacted, provisioningTestKey)
	require.Contains(t, redacted, "Telegram")
}
