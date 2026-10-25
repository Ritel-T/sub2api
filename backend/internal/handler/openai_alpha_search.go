package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/deepseektools"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// AlphaSearch proxies the standalone search endpoint used by Codex Responses Lite.
func (h *OpenAIGatewayHandler) AlphaSearch(c *gin.Context) {
	h.alphaSearch(c, nil)
}

func (h *OpenAIGatewayHandler) alphaSearch(c *gin.Context, bridge *deepSeekToolsBridge) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	setOpenAIClientTransportHTTP(c)
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if !alphaSearchGroupAllowed(apiKey, bridge) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search is only available for OpenAI and Composite groups")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.alpha_search",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	requestedModel := strings.TrimSpace(modelResult.String())
	if !compositeTargetPlatformAllowed(c, apiKey, requestedModel, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search only supports OpenAI models for Composite groups")
		return
	}
	reqLog = reqLog.With(zap.String("model", requestedModel))
	setOpsRequestContext(c, requestedModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if decision := h.checkSecurityAudit(c, reqLog, service.SubscriptionOverflowSourceAPIKey(apiKey), subject, "openai_alpha_search", requestedModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()
	finishRouteResolution := middleware2.BeginSubscriptionBillingRouteResolution(c)
	defer finishRouteResolution()
	userRelease, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, apiKey.ID, apiKey.ConcurrencyLimit, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}

	var subscription *service.UserSubscription
	apiKey, subscription, err = resolveRequestSubscriptionBilling(c, h.apiKeyService, h.billingCacheService, body, h.checkRegionalBillingEligibility, func(key *service.APIKey) *securityaudit.Decision {
		return h.checkSecurityAudit(c, reqLog, key, subject, "openai_alpha_search", requestedModel, body)
	})
	if err != nil {
		if audit, ok := err.(*subscriptionOverflowAuditError); ok {
			h.openAISecurityAuditError(c, audit.decision)
			return
		}
		status, code, message, retryAfter := subscriptionBillingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	if !alphaSearchGroupAllowed(apiKey, bridge) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search is only available for OpenAI and Composite groups")
		return
	}
	ensureCompositeTargetPlatform(c, apiKey, requestedModel)
	if !compositeTargetPlatformAllowed(c, apiKey, requestedModel, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search only supports OpenAI models for Composite groups")
		return
	}
	schedulingGroupID := apiKey.GroupID
	if bridge != nil {
		schedulingGroupID = &bridge.groupID
	}
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), schedulingGroupID, requestedModel)
	forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
	c.Request = c.Request.WithContext(service.WithOpenAIAlphaSearchForwardModel(c.Request.Context(), openAIChannelForwardModel(channelMapping, requestedModel)))

	// 余额模式在途预留（与计费同口径估算；计费任务扣减余额缓存后才释放）。
	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, service.InflightEstimateRequest{Model: requestedModel, BodyBytes: len(body), MaxTokens: requestMaxOutputTokens(body), Kind: service.InflightEstimateToken, SearchCalls: 1})
	if inflightErr != nil {
		status, code, message, retryAfter := billingErrorDetails(inflightErr)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer inflightDone()

	searchID := strings.TrimSpace(gjson.GetBytes(body, "id").String())
	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(c, nil, searchID)
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var rpmAdmission openAIRPMAdmission
	switchCount := 0
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	// 分组利润控制：alpha search 文本入口请求级装门并固定 pricingAt
	//（记录路径经 service.OpenAIPricingAtFromContext 从请求 ctx 回读）。
	schedulingCtx := c.Request.Context()
	if bridge != nil {
		schedulingCtx, err = h.gatewayService.DeepSeekToolsSchedulingContext(schedulingCtx, bridge.groupID)
		if err != nil {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Search backend is unavailable")
			return
		}
	}
	asPricingCtx, _ := h.gatewayService.WithOpenAIRequestPricingContext(schedulingCtx, schedulingGroupID)
	// Group identity remains the caller's; the pool ID is separate. Freeze the
	// same pricing instant for selection, reservation and persisted billing.
	c.Request = c.Request.WithContext(asPricingCtx)
	if bridge != nil {
		asPricingCtx = service.WithAlphaSearchResponseTransform(asPricingCtx, func(raw []byte) ([]byte, bool, error) {
			return deepseektools.AdaptResponse(raw, bridge.request)
		})
	}

	for {
		selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
			asPricingCtx,
			schedulingGroupID,
			"",
			sessionHash,
			requestedModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportHTTPSSE,
			service.OpenAIEndpointCapabilityAlphaSearch,
			false,
			false,
			false,
			service.PlatformOpenAI,
		)
		if err != nil || selection == nil || selection.Account == nil {
			err = rpmAdmission.selectionError(err)
			if isOpenAIRPMError(err) {
				rpmAdmission.retryAfter(c, err)
				cls := classifySelectionFailureError(err, noAccountErrorClassification{})
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return
			}
			if failoverClientGone(c) {
				reqLog.Info("openai_alpha_search.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if bridge != nil {
				if lastFailoverErr != nil {
					h.handleAlphaSearchFailoverExhausted(c, lastFailoverErr, false, bridge)
				} else {
					h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Search backend is unavailable")
				}
				return
			}
			if len(failedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestedModel, requestedModel, service.PlatformOpenAI)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
				return
			}
			if lastFailoverErr != nil {
				h.handleAlphaSearchFailoverExhausted(c, lastFailoverErr, false, bridge)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			return
		}

		account := selection.Account
		if !deepSeekToolsAccountAllowed(bridge, account) {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			failedAccountIDs[account.ID] = struct{}{}
			continue
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)
		accountRelease, slotResult := h.acquireResponsesAccountSlot(c, schedulingGroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// 利润终检否决：排除该账号重新选号；否决次数达上限则按无可用账号终止。
			if !recordOpenAIProfitVeto(failedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
			return
		}
		retryRPM, rpmErr := rpmAdmission.acquire(c.Request.Context(), h.gatewayService, account, accountRelease, failedAccountIDs)
		if retryRPM {
			continue
		}
		if rpmErr != nil {
			cls := classifySelectionFailureError(rpmErr, noAccountErrorClassification{})
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		writerSizeBeforeForward := c.Writer.Size()
		forwardStart := time.Now()
		var result *service.OpenAIForwardResult
		result, err = func() (*service.OpenAIForwardResult, error) {
			if accountRelease != nil {
				defer accountRelease()
			}
			return h.gatewayService.ForwardAlphaSearch(rpmAdmission.forwardContext(asPricingCtx, account), c, account, forwardBody)
		}()
		if h.handleOpenAIRPMForwardError(c, err, streamStarted, false) {
			return
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, time.Since(forwardStart).Milliseconds())

		if err == nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestedModel, false, result), true, nil)
			if result != nil && (bridge == nil || result.WebSearchCalls > 0) {
				if bridge != nil {
					result.Model = "deepseek-" + bridge.request.Operation
					requestedModel = result.Model
					body = bridge.originalBody
					channelMapping = service.ChannelMappingResult{}
					if result.UpstreamEndpoint == "" {
						result.UpstreamEndpoint = "/v1/alpha/search"
					}
				}
				h.recordAlphaSearchUsage(c, apiKey, account, subscription, channelMapping, requestedModel, body, result, subject.UserID, bridge != nil)
			}
			return
		}

		var failoverErr *service.UpstreamFailoverError
		if !errors.As(err, &failoverErr) {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestedModel, false, result), false, nil, err)
			if failoverClientGone(c) {
				reqLog.Info("openai_alpha_search.forward_aborted_client_disconnected", zap.Int64("account_id", account.ID), zap.Error(err))
				return
			}
			if c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("openai_alpha_search.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, requestedModel, false, result), false, nil, err)
		if c.Writer.Size() != writerSizeBeforeForward {
			h.handleAlphaSearchFailoverExhausted(c, failoverErr, true, bridge)
			return
		}
		if failoverClientGone(c) {
			reqLog.Info("openai_alpha_search.failover_aborted_client_disconnected",
				zap.Int64("account_id", account.ID),
				zap.Int("upstream_status", failoverErr.StatusCode),
			)
			return
		}
		if failoverErr.RetryableOnSameAccount {
			retryLimit := account.GetPoolModeRetryCount()
			if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
				sameAccountRetryCount[account.ID]++
				retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
				reqLog.Warn("openai_alpha_search.same_account_retry",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("retry_limit", retryLimit),
					zap.Int("retry_count", sameAccountRetryCount[account.ID]),
					zap.Duration("retry_delay", retryDelay),
				)
				select {
				case <-c.Request.Context().Done():
					return
				case <-time.After(retryDelay):
				}
				continue
			}
		}
		h.gatewayService.RecordOpenAIAccountSwitch()
		failedAccountIDs[account.ID] = struct{}{}
		lastFailoverErr = failoverErr
		if switchCount >= h.maxAccountSwitches {
			h.handleAlphaSearchFailoverExhausted(c, failoverErr, false, bridge)
			return
		}
		switchCount++
		if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
			h.handleAlphaSearchFailoverExhausted(c, failoverErr, false, bridge)
			return
		}
		reqLog.Warn("openai_alpha_search.upstream_failover_switching",
			zap.Int64("account_id", account.ID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("switch_count", switchCount),
		)
	}
}

// recordAlphaSearchUsage 为一次成功的 alpha/search 网页搜索落按次计费用量行
// （上游不返回 usage 字段，按 WebSearchCalls 走分组单价 × 倍率的按次口径）。
// 与 images 一致使用 mandatory 池提交，池满时同步兜底执行，保证扣费不丢。
func (h *OpenAIGatewayHandler) recordAlphaSearchUsage(
	c *gin.Context,
	apiKey *service.APIKey,
	account *service.Account,
	subscription *service.UserSubscription,
	channelMapping service.ChannelMappingResult,
	requestedModel string,
	body []byte,
	result *service.OpenAIForwardResult,
	userID int64,
	preserveCallerMultiplier bool,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:                        result,
			PreserveCallerGroupMultiplier: preserveCallerMultiplier,
			APIKey:                        apiKey,
			User:                          apiKey.User,
			Account:                       account,
			Subscription:                  subscription,
			InboundEndpoint:               inboundEndpoint,
			UpstreamEndpoint:              upstreamEndpoint,
			UserAgent:                     userAgent,
			IPAddress:                     clientIP,
			RequestPayloadHash:            requestPayloadHash,
			APIKeyService:                 h.apiKeyService,
			QuotaPlatform:                 quotaPlatform,
			SessionID:                     sessionID,
			ChannelUsageFields:            channelMapping.ToUsageFields(requestedModel, result.UpstreamModel),
			PricingAt:                     service.OpenAIPricingAtFromContext(c.Request.Context()),
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.alpha_search"),
				zap.Int64("user_id", userID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestedModel),
				zap.Int64("account_id", account.ID),
			).Error("openai_alpha_search.record_usage_failed", zap.Error(err))
		}
	})
}
