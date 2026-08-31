package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ScheduledTestHandler handles admin scheduled-test-plan management.
type ScheduledTestHandler struct {
	scheduledTestSvc *service.ScheduledTestService
}

// NewScheduledTestHandler creates a new ScheduledTestHandler.
func NewScheduledTestHandler(scheduledTestSvc *service.ScheduledTestService) *ScheduledTestHandler {
	return &ScheduledTestHandler{scheduledTestSvc: scheduledTestSvc}
}

type createScheduledTestPlanRequest struct {
	AccountID      int64  `json:"account_id" binding:"required"`
	ModelID        string `json:"model_id"`
	CronExpression string `json:"cron_expression" binding:"required"`
	Enabled        *bool  `json:"enabled"`
	MaxResults     int    `json:"max_results"`
	AutoRecover    *bool  `json:"auto_recover"`
}

type updateScheduledTestPlanRequest struct {
	ModelID        string `json:"model_id"`
	CronExpression string `json:"cron_expression"`
	Enabled        *bool  `json:"enabled"`
	MaxResults     int    `json:"max_results"`
	AutoRecover    *bool  `json:"auto_recover"`
}

type setManagedScheduledTestTemplateEnabledRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

type setManagedScheduledTestAccountOptOutRequest struct {
	Disabled *bool `json:"disabled" binding:"required"`
}

type updateManagedScheduledTestTemplateRequest struct {
	ModelID          *string `json:"model_id"`
	IntervalMinutes  *int    `json:"interval_minutes"`
	ShardStartMinute *int    `json:"shard_start_minute"`
	ShardCount       *int    `json:"shard_count"`
	AutoRecover      *bool   `json:"auto_recover"`
	OnlyWhenBlocked  *bool   `json:"only_when_blocked"`
	MaxResults       *int    `json:"max_results"`
}

// ListByAccount GET /admin/accounts/:id/scheduled-test-plans
func (h *ScheduledTestHandler) ListByAccount(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}

	plans, err := h.scheduledTestSvc.ListPlansByAccount(c.Request.Context(), accountID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, plans)
}

// Create POST /admin/scheduled-test-plans
func (h *ScheduledTestHandler) Create(c *gin.Context) {
	var req createScheduledTestPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	plan := &service.ScheduledTestPlan{
		AccountID:      req.AccountID,
		ModelID:        req.ModelID,
		CronExpression: req.CronExpression,
		Enabled:        true,
		MaxResults:     req.MaxResults,
	}
	if req.Enabled != nil {
		plan.Enabled = *req.Enabled
	}
	if req.AutoRecover != nil {
		plan.AutoRecover = *req.AutoRecover
	}

	created, err := h.scheduledTestSvc.CreatePlan(c.Request.Context(), plan)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, created)
}

// Update PUT /admin/scheduled-test-plans/:id
func (h *ScheduledTestHandler) Update(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}

	existing, err := h.scheduledTestSvc.GetPlan(c.Request.Context(), planID)
	if err != nil {
		response.NotFound(c, "plan not found")
		return
	}

	var req updateScheduledTestPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if req.ModelID != "" {
		existing.ModelID = req.ModelID
	}
	if req.CronExpression != "" {
		existing.CronExpression = req.CronExpression
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.MaxResults > 0 {
		existing.MaxResults = req.MaxResults
	}
	if req.AutoRecover != nil {
		existing.AutoRecover = *req.AutoRecover
	}

	updated, err := h.scheduledTestSvc.UpdatePlan(c.Request.Context(), existing)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, updated)
}

// Delete DELETE /admin/scheduled-test-plans/:id
func (h *ScheduledTestHandler) Delete(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}

	if err := h.scheduledTestSvc.DeletePlan(c.Request.Context(), planID); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ListResults GET /admin/scheduled-test-plans/:id/results
func (h *ScheduledTestHandler) ListResults(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}

	results, err := h.scheduledTestSvc.ListResults(c.Request.Context(), planID, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, results)
}

// PreviewManagedTemplate GET /admin/scheduled-test-defaults/:template_key/preview
func (h *ScheduledTestHandler) PreviewManagedTemplate(c *gin.Context) {
	report, err := h.scheduledTestSvc.PreviewManagedTemplate(c.Request.Context(), c.Param("template_key"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, report)
}

// ReconcileManagedTemplate POST /admin/scheduled-test-defaults/:template_key/reconcile
func (h *ScheduledTestHandler) ReconcileManagedTemplate(c *gin.Context) {
	report, err := h.scheduledTestSvc.ReconcileManagedTemplate(c.Request.Context(), c.Param("template_key"), true)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if idempotencyKey := c.GetHeader("Idempotency-Key"); idempotencyKey != "" {
		c.Header("Idempotency-Key", idempotencyKey)
	}
	c.JSON(http.StatusOK, report)
}

// ManagedTemplateStatus GET /admin/scheduled-test-defaults/:template_key/status
func (h *ScheduledTestHandler) ManagedTemplateStatus(c *gin.Context) {
	status, err := h.scheduledTestSvc.ManagedTemplateStatus(c.Request.Context(), c.Param("template_key"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, status)
}

// SetManagedTemplateEnabled PUT /admin/scheduled-test-defaults/:template_key/enabled
func (h *ScheduledTestHandler) SetManagedTemplateEnabled(c *gin.Context) {
	var req setManagedScheduledTestTemplateEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		response.BadRequest(c, "enabled is required")
		return
	}
	template, err := h.scheduledTestSvc.SetManagedTemplateEnabled(c.Request.Context(), c.Param("template_key"), *req.Enabled)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, template)
}

// SetManagedAccountOptOut PUT /admin/scheduled-test-defaults/:template_key/accounts/:id/opt-out
func (h *ScheduledTestHandler) SetManagedAccountOptOut(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}
	var req setManagedScheduledTestAccountOptOutRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Disabled == nil {
		response.BadRequest(c, "disabled is required")
		return
	}
	report, err := h.scheduledTestSvc.SetManagedAccountOptOut(c.Request.Context(), c.Param("template_key"), accountID, *req.Disabled)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, report)
}

// UpdateManagedTemplate PUT /admin/scheduled-test-defaults/:template_key
func (h *ScheduledTestHandler) UpdateManagedTemplate(c *gin.Context) {
	template, err := h.scheduledTestSvc.GetManagedTemplate(c.Request.Context(), c.Param("template_key"))
	if err != nil {
		response.NotFound(c, "managed template not found")
		return
	}
	var req updateManagedScheduledTestTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.ModelID != nil {
		template.ModelID = *req.ModelID
	}
	if req.IntervalMinutes != nil {
		template.IntervalMinutes = *req.IntervalMinutes
	}
	if req.ShardStartMinute != nil {
		template.ShardStartMinute = *req.ShardStartMinute
	}
	if req.ShardCount != nil {
		template.ShardCount = *req.ShardCount
	}
	if req.AutoRecover != nil {
		template.AutoRecover = *req.AutoRecover
	}
	if req.OnlyWhenBlocked != nil {
		template.OnlyWhenBlocked = *req.OnlyWhenBlocked
	}
	if req.MaxResults != nil {
		template.MaxResults = *req.MaxResults
	}
	updated, err := h.scheduledTestSvc.UpdateManagedTemplate(c.Request.Context(), template)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, updated)
}
