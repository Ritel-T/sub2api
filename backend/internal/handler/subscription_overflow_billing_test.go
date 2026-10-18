package handler

import (
	"context"
	"errors"
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

func TestOverflowResolutionRejectsTargetPolicyBeforeBilling(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetDeny bool
		auditDeny  bool
	}{
		{name: "target model denied", targetDeny: true}, {name: "target audit denied", auditDeny: true}, {name: "successful route"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fallbackID := int64(2)
			source := &service.APIKey{ID: 3, UserID: 7, Group: &service.Group{ID: 1, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionFallbackGroupID: &fallbackID}, User: &service.User{ID: 7}}
			groupID := int64(2)
			target := *source
			target.GroupID = &groupID
			target.Group = &service.Group{ID: 2, Platform: service.PlatformOpenAI}
			if tc.targetDeny {
				target.Group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6-luna"}}
			}
			target.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroupID: 1, TargetGroupID: 2, SourceGroup: source.Group}
			router := gin.New()
			checks, audits, resolves := 0, 0, 0
			router.POST("/v1/responses", func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyAPIKey), source)
				c.Request = c.Request.WithContext(service.WithSubscriptionBillingResolver(c.Request.Context(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
					resolves++
					return &target, nil, nil
				}))
				key, sub, err := resolveRequestSubscriptionBilling(c, nil, nil, []byte(`{"model":"gpt-6-sol"}`), func(ctx context.Context, key *service.APIKey, sub *service.UserSubscription) error {
					checks++
					require.Same(t, &target, key)
					require.Nil(t, sub)
					return nil
				}, func(key *service.APIKey) *securityaudit.Decision {
					audits++
					return &securityaudit.Decision{Kind: securityaudit.DecisionBlock, AllowNextStage: !tc.auditDeny}
				})
				if tc.targetDeny || tc.auditDeny {
					require.Error(t, err)
					require.Nil(t, key)
					require.Equal(t, 0, checks)
				} else {
					require.NoError(t, err)
					require.Same(t, &target, key)
					require.Nil(t, sub)
					require.Equal(t, 1, checks)
				}
				if tc.targetDeny {
					require.Zero(t, audits)
				} else {
					require.Equal(t, 1, audits)
				}
				require.Equal(t, 1, resolves)
				require.False(t, c.Writer.Written())
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
		})
	}
}

func TestOpenAIOverflowTargetDeniedReleasesUserSlotWithoutSelectingAccount(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		t.Run(path, func(t *testing.T) {
			cache := &helperConcurrencyCacheStub{userSeq: []bool{true}}
			h := newServiceTierHandlerTest(t)
			h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			sourceID, targetID := int64(1), int64(2)
			source := &service.APIKey{ID: 3, UserID: 7, GroupID: &sourceID, Group: &service.Group{ID: 1, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionFallbackGroupID: &targetID}, User: &service.User{ID: 7, Status: service.StatusActive}}
			target := *source
			target.GroupID = &targetID
			target.Group = &service.Group{ID: 2, Platform: service.PlatformOpenAI, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6-luna"}}}
			target.SubscriptionOverflow = &service.SubscriptionOverflowEvidence{SourceGroup: source.Group}
			router := gin.New()
			router.POST(path, func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyAPIKey), source)
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
				c.Request = c.Request.WithContext(service.WithSubscriptionBillingResolver(c.Request.Context(), func(context.Context, *service.APIKey) (*service.APIKey, *service.UserSubscription, error) {
					return &target, nil, nil
				}))
				if path == "/v1/responses" {
					h.Responses(c)
				} else {
					h.ChatCompletions(c)
				}
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-6-sol","stream":true,"input":"hi","messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Contains(t, rec.Body.String(), "not available")
			require.Equal(t, 1, cache.userAcquireCalls)
			require.Equal(t, 1, cache.userReleaseCalls)
			require.Zero(t, cache.accountAcquireCalls)
		})
	}
}

func TestOverflowGroupAuditDoesNotReuseSourceSuccess(t *testing.T) {
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking, decisions: []*securityaudit.PromptDecision{{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, {Kind: securityaudit.DecisionBlock, AllowNextStage: false}}}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	sourceID, targetID := int64(1), int64(2)
	source := &service.APIKey{GroupID: &sourceID}
	target := &service.APIKey{GroupID: &targetID, SubscriptionOverflow: &service.SubscriptionOverflowEvidence{SourceGroupID: 1}}
	first := runSecurityAudit(c, nil, coordinator, nil, source, middleware2.AuthSubject{}, "openai_responses", "gpt-6-sol", nil, "http")
	require.True(t, first.AllowNextStage)
	require.Nil(t, runSecurityAudit(c, nil, coordinator, nil, source, middleware2.AuthSubject{}, "openai_responses", "gpt-6-sol", nil, "http"))
	second := runSecurityAudit(c, nil, coordinator, nil, target, middleware2.AuthSubject{}, "openai_responses", "gpt-6-sol", nil, "http")
	require.False(t, second.AllowNextStage)
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestOverflowNeverSuppressesNonSubscriptionBillingError(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{User: &service.User{ID: 1}})
	calls := 0
	sentinel := errors.New("wallet unavailable")
	_, _, err := resolveRequestSubscriptionBilling(c, nil, nil, nil, func(context.Context, *service.APIKey, *service.UserSubscription) error { calls++; return sentinel }, func(*service.APIKey) *securityaudit.Decision { t.Fatal("unexpected audit"); return nil })
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, calls)
}
