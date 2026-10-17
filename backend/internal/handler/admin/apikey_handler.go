package admin

import (
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminAPIKeyHandler handles admin API key management
type AdminAPIKeyHandler struct {
	adminService  service.AdminService
	apiKeyService *service.APIKeyService
}

// NewAdminAPIKeyHandler creates a new admin API key handler
func NewAdminAPIKeyHandler(adminService service.AdminService, apiKeyService *service.APIKeyService) *AdminAPIKeyHandler {
	return &AdminAPIKeyHandler{
		adminService:  adminService,
		apiKeyService: apiKeyService,
	}
}

// AdminCreateUserAPIKeyRequest exposes only the fields needed for provisioning.
// ExpectedPassword is optional for key-only administration. When supplied, it
// verifies a saved provisioning password without resetting the user's password.
type AdminCreateUserAPIKeyRequest struct {
	Name             string  `json:"name"`
	GroupID          *int64  `json:"group_id"`
	CustomKey        *string `json:"custom_key"`
	ExpectedPassword *string `json:"expected_password"`
}

// CreateForUser creates a key using the target user's existing group permissions.
// POST /api/v1/admin/users/:id/api-keys
func (h *AdminAPIKeyHandler) CreateForUser(c *gin.Context) {
	if !requireProvisioningAdmin(c) {
		return
	}
	userID, ok := provisioningUserID(c)
	if !ok {
		return
	}
	var req AdminCreateUserAPIKeyRequest
	if !bindProvisioningJSON(c, &req) {
		return
	}
	if req.Name == "" || len(req.Name) > 100 ||
		(req.GroupID != nil && *req.GroupID <= 0) ||
		(req.ExpectedPassword != nil && *req.ExpectedPassword == "") {
		response.BadRequest(c, "Invalid request")
		return
	}
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if key == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	// Provisioning must not silently degrade to a non-idempotent create.
	if service.DefaultIdempotencyCoordinator() == nil {
		response.ErrorFrom(c, service.ErrIdempotencyStoreUnavail)
		return
	}
	user, err := h.provisioningUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.ExpectedPassword != nil && !user.CheckPassword(*req.ExpectedPassword) {
		response.ErrorFrom(c, infraerrors.Conflict("USER_PASSWORD_MISMATCH", "Saved password does not match the current user password"))
		return
	}
	idempotencyPayload := struct {
		UserID int64                        `json:"user_id"`
		Body   AdminCreateUserAPIKeyRequest `json:"body"`
	}{UserID: userID, Body: req}
	executeAdminIdempotentJSON(c, "admin.users.api_keys.create", idempotencyPayload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		apiKey, err := h.apiKeyService.Create(ctx, userID, service.CreateAPIKeyRequest{
			Name: req.Name, GroupID: req.GroupID, CustomKey: req.CustomKey,
		})
		if err != nil {
			return nil, err
		}
		return dto.APIKeyFromService(apiKey), nil
	})
}

// VerifyUserPassword checks a provisioning password without logging in, issuing
// tokens, updating activity, or changing public CAPTCHA requirements.
// POST /api/v1/admin/users/:id/verify-password
func (h *AdminAPIKeyHandler) VerifyUserPassword(c *gin.Context) {
	if !requireProvisioningAdmin(c) {
		return
	}
	userID, ok := provisioningUserID(c)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !bindProvisioningJSON(c, &req) {
		return
	}
	if req.Password == "" {
		response.BadRequest(c, "Invalid request")
		return
	}
	user, err := h.provisioningUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"user_id": userID, "valid": user.CheckPassword(req.Password)})
}

func requireProvisioningAdmin(c *gin.Context) bool {
	if subject, ok := middleware.GetAuthSubjectFromContext(c); !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Admin authentication required")
		return false
	}
	if c.GetString(string(middleware.ContextKeyUserRole)) != service.RoleAdmin {
		response.Forbidden(c, "Admin access required")
		return false
	}
	return true
}

func provisioningUserID(c *gin.Context) (int64, bool) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return 0, false
	}
	return userID, true
}

func bindProvisioningJSON(c *gin.Context, out any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		response.BadRequest(c, "Invalid request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Invalid request")
		return false
	}
	return true
}

func (h *AdminAPIKeyHandler) provisioningUser(ctx context.Context, userID int64) (*service.User, error) {
	user, err := h.adminService.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.DeletedAt != nil {
		return nil, service.ErrUserNotFound
	}
	if user.Role != service.RoleUser || !user.IsActive() {
		return nil, infraerrors.BadRequest("INVALID_PROVISIONING_USER", "Target must be an active normal user")
	}
	return user, nil
}

// AdminUpdateAPIKeyGroupRequest represents the request to update an API key.
type AdminUpdateAPIKeyGroupRequest struct {
	GroupID             *int64 `json:"group_id"`               // nil=不修改, 0=解绑, >0=绑定到目标分组
	ResetRateLimitUsage *bool  `json:"reset_rate_limit_usage"` // true=重置 5h/1d/7d 限速用量
}

// UpdateGroup handles updating an API key's admin-managed fields.
// PUT /api/v1/admin/api-keys/:id
func (h *AdminAPIKeyHandler) UpdateGroup(c *gin.Context) {
	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid API key ID")
		return
	}

	var req AdminUpdateAPIKeyGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	var resetKey *service.APIKey
	if req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage {
		resetKey, err = h.adminService.AdminResetAPIKeyRateLimitUsage(c.Request.Context(), keyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	result, err := h.adminService.AdminUpdateAPIKeyGroupID(c.Request.Context(), keyID, req.GroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if resetKey != nil && req.GroupID == nil {
		result.APIKey = resetKey
	}

	resp := struct {
		APIKey                 *dto.APIKey `json:"api_key"`
		AutoGrantedGroupAccess bool        `json:"auto_granted_group_access"`
		GrantedGroupID         *int64      `json:"granted_group_id,omitempty"`
		GrantedGroupName       string      `json:"granted_group_name,omitempty"`
	}{
		APIKey:                 dto.APIKeyFromService(result.APIKey),
		AutoGrantedGroupAccess: result.AutoGrantedGroupAccess,
		GrantedGroupID:         result.GrantedGroupID,
		GrantedGroupName:       result.GrantedGroupName,
	}
	response.Success(c, resp)
}
