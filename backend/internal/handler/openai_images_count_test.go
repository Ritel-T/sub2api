//go:build unit

package handler

import (
	"bytes"
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIGatewayHandlerImages_UnsupportedBatchStopsBeforeForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(3130)
	accounts := []service.Account{
		{
			ID:          1,
			Name:        "image-account-1",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    0,
			Credentials: map[string]any{"access_token": "token-1"},
		},
		{
			ID:          2,
			Name:        "image-account-2",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    1,
			Credentials: map[string]any{"access_token": "token-2"},
		},
	}
	accountRepo := openAIImagesFailoverAccountRepo{accounts: accounts}
	upstream := &openAIImagesFailoverHTTPUpstream{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	concurrencyService := service.NewConcurrencyService(nil)
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	handler.maxAccountSwitches = 10

	body := []byte(`{"model":"gpt-image-2.5","prompt":"draw a cat","quality":"high","size":"1536x1024","n":2}`)
	core, observedLogs := observer.New(zap.DebugLevel)
	requestCtx := logger.IntoContext(context.Background(), zap.New(core))
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body)).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})

	handler.Images(c)

	require.Empty(t, upstream.calls(), "local count rejection must not scan any provider account")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, "unsupported_image_count", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
	require.Equal(t, "n", gjson.GetBytes(rec.Body.Bytes(), "error.param").String())
	require.Contains(t, gjson.GetBytes(rec.Body.Bytes(), "error.message").String(), "n=1")
	require.Empty(t, observedLogs.FilterMessage("openai.images.upstream_failover_switching").All())
	require.Empty(t, observedLogs.FilterMessage("openai.images.upstream_user_error").All())
	require.Empty(t, observedLogs.FilterMessage("openai.images.forward_failed").All())
	_, upstreamErrors := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, upstreamErrors)
	require.False(t, service.OpenAIImagesJSONKeepalivePresent(c), "reject before account wait and JSON heartbeat")
}

func TestOpenAIGatewayHandlerImages_RequestValidationStopsJSONKeepalive(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	stop := service.StartOpenAIImagesJSONKeepalive(c, time.Hour)
	defer stop()
	h := &OpenAIGatewayHandler{}
	err := &service.OpenAIImagesRequestValidationError{Code: "unsupported_image_count", Param: "n", Message: "Set n to 1."}
	require.True(t, h.handleOpenAIImagesRequestValidationError(c, err, false))
	require.False(t, service.StopOpenAIImagesJSONKeepaliveCommitted(c))
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
}

func TestOpenAIGatewayHandlerImages_UnsupportedBatchReleasesSchedulerSlotOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	released := 0
	selection := &service.AccountSelectionResult{Account: &service.Account{
		Type: service.AccountTypeOAuth, Platform: service.PlatformOpenAI,
	}, ReleaseFunc: func() { released++ }}
	parsed := &service.OpenAIImagesRequest{Model: "gpt-image-2.5", N: 2}
	h := &OpenAIGatewayHandler{} // No gateway/feedback writer is available on this path.
	require.True(t, h.handleOpenAIImagesSelectedCountValidation(c, selection, parsed, ""))
	require.Equal(t, 1, released)
	require.Nil(t, selection.ReleaseFunc)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "n", gjson.GetBytes(rec.Body.Bytes(), "error.param").String())
}

func TestOpenAIGatewayHandlerImages_RequestValidationAfterUserWaitSSEPing(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Header("Content-Type", "text/event-stream")
	_, writeErr := c.Writer.Write([]byte(": ping\n\n"))
	require.NoError(t, writeErr)
	c.Writer.Flush()
	released := 0
	selection := &service.AccountSelectionResult{Account: &service.Account{Type: service.AccountTypeOAuth}, ReleaseFunc: func() { released++ }}
	parsed := &service.OpenAIImagesRequest{Model: "gpt-image-2.5", N: 2, Stream: true}
	h := &OpenAIGatewayHandler{}
	require.True(t, h.handleOpenAIImagesSelectedCountValidation(c, selection, parsed, ""))
	require.Equal(t, 1, released)
	require.Equal(t, http.StatusOK, rec.Code, "preserve the previously committed SSE status")
	require.Contains(t, rec.Body.String(), "event:error\n")
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			require.Equal(t, "invalid_request_error", gjson.Get(data, "error.type").String())
			require.Equal(t, "unsupported_image_count", gjson.Get(data, "error.code").String())
			require.Equal(t, "n", gjson.Get(data, "error.param").String())
		}
	}
	require.NotContains(t, rec.Body.String(), "\n{\"error\"")
}

func TestOpenAIGatewayHandlerImages_RequestValidationOpsAttribution(t *testing.T) {
	for _, mode := range []string{"before_headers", "json_heartbeat", "sse_ping"} {
		t.Run(mode, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 4)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/images/generations", func(c *gin.Context) {
				setOpsRequestContext(c, "gpt-image-2.5", mode == "sse_ping")
				c.Set(service.OpsUpstreamStatusCodeKey, http.StatusTooManyRequests)
				c.Set(service.OpsUpstreamErrorMessageKey, "earlier attempt was rate limited")
				c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{UpstreamStatusCode: http.StatusTooManyRequests, Message: "earlier attempt was rate limited"}})
				if mode == "json_heartbeat" {
					stop := service.StartOpenAIImagesJSONKeepalive(c, time.Millisecond)
					defer stop()
					// Wait for a real whitespace beat; no network or provider fixture.
					time.Sleep(10 * time.Millisecond)
				}
				if mode == "sse_ping" {
					c.Header("Content-Type", "text/event-stream")
					_, _ = c.Writer.Write([]byte(": ping\n\n"))
					c.Writer.Flush()
				}
				h := &OpenAIGatewayHandler{}
				require.True(t, h.handleOpenAIImagesRequestValidationError(c, &service.OpenAIImagesRequestValidationError{Code: "unsupported_image_count", Param: "n", Message: "Set n to 1."}, mode == "sse_ping"))
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil))
			require.Equal(t, int64(2), OpsErrorLogQueueLength())
			local := <-opsErrorLogQueue
			require.Equal(t, "client", local.entry.ErrorOwner)
			require.Equal(t, "request", local.entry.ErrorPhase)
			require.Equal(t, "client_request", local.entry.ErrorSource)
			require.Equal(t, http.StatusBadRequest, local.entry.StatusCode)
			require.Equal(t, mode == "sse_ping", local.entry.Stream)
			require.Contains(t, local.entry.ErrorBody, "unsupported_image_count")
			require.Nil(t, local.entry.UpstreamStatusCode)
			provider := <-opsErrorLogQueue
			require.Equal(t, "provider", provider.entry.ErrorOwner)
			require.Equal(t, "upstream", provider.entry.ErrorPhase)
			require.Contains(t, provider.entry.ErrorMessage, "Prior upstream error before local request rejection")
			require.NotContains(t, provider.entry.ErrorMessage, "Recovered")
			require.NotNil(t, provider.entry.UpstreamStatusCode)
			require.Equal(t, http.StatusTooManyRequests, *provider.entry.UpstreamStatusCode)
		})
	}
}
