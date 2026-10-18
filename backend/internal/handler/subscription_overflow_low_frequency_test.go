package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type lowFrequencyOverflowAuditEngine struct {
	blockSource bool
	requests    []securityaudit.Request
}

func (*lowFrequencyOverflowAuditEngine) EffectiveMode() securityaudit.Mode {
	return securityaudit.ModeBlocking
}
func (*lowFrequencyOverflowAuditEngine) Enqueue(context.Context, securityaudit.Request) error {
	return nil
}
func (e *lowFrequencyOverflowAuditEngine) Evaluate(_ context.Context, request securityaudit.Request) (*securityaudit.PromptDecision, error) {
	e.requests = append(e.requests, request.Clone())
	if len(e.requests) == 1 && !e.blockSource {
		return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
	}
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionBlock, ErrorCode: securityaudit.ErrorCodeBlocked, AllowNextStage: false}, nil
}

func TestLowFrequencyOverflowPreservesPromptAuditBeforeAdmissionAndForTarget(t *testing.T) {
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "overflow test prompt"))
	image, err := writer.CreateFormFile("image", "input.png")
	require.NoError(t, err)
	_, err = image.Write([]byte("fixture image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	for _, tc := range []struct {
		path, body, contentType, platform, protocol string
		handle                                      func(*OpenAIGatewayHandler, *GatewayHandler, *gin.Context)
	}{
		{"/v1/embeddings", `{"model":"text-embedding-3-small","input":"overflow test prompt"}`, "application/json", service.PlatformOpenAI, "openai_embeddings", func(h *OpenAIGatewayHandler, _ *GatewayHandler, c *gin.Context) { h.Embeddings(c) }},
		{"/v1/responses/alpha/search", `{"model":"gpt-6-sol","query":"overflow test prompt"}`, "application/json", service.PlatformOpenAI, "openai_alpha_search", func(h *OpenAIGatewayHandler, _ *GatewayHandler, c *gin.Context) { h.AlphaSearch(c) }},
		{"/v1/images/generations", `{"model":"gpt-image-2","prompt":"overflow test prompt","stream":true}`, "application/json", service.PlatformOpenAI, service.ContentModerationProtocolOpenAIImages, func(h *OpenAIGatewayHandler, _ *GatewayHandler, c *gin.Context) { h.Images(c) }},
		{"/v1/images/edits", multipartBody.String(), writer.FormDataContentType(), service.PlatformOpenAI, service.ContentModerationProtocolOpenAIImages, func(h *OpenAIGatewayHandler, _ *GatewayHandler, c *gin.Context) { h.Images(c) }},
		{"/v1/videos/generations", `{"model":"grok-imagine-video","prompt":"overflow test prompt"}`, "application/json", service.PlatformGrok, service.ContentModerationProtocolOpenAIImages, func(h *OpenAIGatewayHandler, _ *GatewayHandler, c *gin.Context) { h.GrokVideoGeneration(c) }},
		{"/v1/web_search", `{"query":"overflow test prompt"}`, "application/json", service.PlatformGrok, service.ContentModerationProtocolOpenAIChat, func(_ *OpenAIGatewayHandler, h *GatewayHandler, c *gin.Context) { h.WebSearch(c) }},
		{"/v1/x_search", `{"query":"overflow test prompt"}`, "application/json", service.PlatformGrok, service.ContentModerationProtocolOpenAIChat, func(_ *OpenAIGatewayHandler, h *GatewayHandler, c *gin.Context) { h.XSearch(c) }},
	} {
		for _, blockSource := range []bool{false, true} {
			name := tc.path + "/target-blocked"
			if blockSource {
				name = tc.path + "/source-blocked"
			}
			t.Run(name, func(t *testing.T) {
				cache := &helperConcurrencyCacheStub{userSeq: []bool{true}}
				engine := &lowFrequencyOverflowAuditEngine{blockSource: blockSource}
				coordinator := securityaudit.NewCoordinator(nil, engine)
				h := newServiceTierHandlerTest(t)
				h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
				h.securityAuditCoordinator = coordinator
				gateway := &GatewayHandler{apiKeyService: h.apiKeyService, billingCacheService: h.billingCacheService, concurrencyHelper: h.concurrencyHelper, securityAuditCoordinator: coordinator}
				sourceID, targetID := int64(1), int64(2)
				source := &service.APIKey{ID: 3, UserID: 7, GroupID: &sourceID,
					Group: &service.Group{ID: sourceID, Platform: tc.platform, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionFallbackGroupID: &targetID, AllowImageGeneration: true},
					User:  &service.User{ID: 7, Status: service.StatusActive}}
				target := *source
				target.GroupID = &targetID
				target.Group = &service.Group{ID: targetID, Platform: tc.platform, AllowImageGeneration: true}
				target.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroupID: sourceID, TargetGroupID: targetID, SourceGroup: source.Group}
				resolves := 0
				router := gin.New()
				router.POST(tc.path, func(c *gin.Context) {
					c.Set(string(middleware2.ContextKeyAPIKey), source)
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
					c.Request = c.Request.WithContext(service.WithSubscriptionBillingResolver(c.Request.Context(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
						resolves++
						require.Equal(t, 1, cache.userAcquireCalls, "refresh must follow user admission")
						require.Len(t, engine.requests, 1, "source audit must precede admission and refresh")
						require.False(t, c.Writer.Written(), "admission must not commit an SSE ping")
						return &target, nil, nil
					}))
					tc.handle(h, gateway, c)
				})
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
				request.Header.Set("Content-Type", tc.contentType)
				router.ServeHTTP(recorder, request)
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Contains(t, recorder.Body.String(), securityaudit.ErrorCodeBlocked)
				require.Zero(t, cache.accountAcquireCalls)
				if blockSource {
					require.Zero(t, resolves)
					require.Zero(t, cache.userAcquireCalls)
					require.Zero(t, cache.userReleaseCalls)
					require.Len(t, engine.requests, 1)
				} else {
					require.Equal(t, 1, resolves)
					require.Equal(t, 1, cache.userReleaseCalls)
					require.Len(t, engine.requests, 2)
					require.Equal(t, sourceID, *engine.requests[0].GroupID)
					require.Equal(t, targetID, *engine.requests[1].GroupID)
					require.Equal(t, engine.requests[0].Body, engine.requests[1].Body, "target must audit the same normalized prompt and media")
					require.Contains(t, string(engine.requests[1].Body), "overflow test prompt")
					require.Equal(t, tc.protocol, engine.requests[0].Protocol)
					require.Equal(t, tc.protocol, engine.requests[1].Protocol)
				}
			})
		}
	}
}
