//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type overflowWSGroupAuditEngine struct {
	mu           sync.Mutex
	blockedGroup int64
	groups       []int64
}

func (*overflowWSGroupAuditEngine) EffectiveMode() securityaudit.Mode {
	return securityaudit.ModeBlocking
}
func (*overflowWSGroupAuditEngine) Enqueue(context.Context, securityaudit.Request) error { return nil }
func (e *overflowWSGroupAuditEngine) Evaluate(_ context.Context, req securityaudit.Request) (*securityaudit.PromptDecision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	group := int64(0)
	if req.GroupID != nil {
		group = *req.GroupID
	}
	e.groups = append(e.groups, group)
	if group == e.blockedGroup {
		return &securityaudit.PromptDecision{Kind: securityaudit.DecisionBlock}, nil
	}
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
}

func TestSubscriptionOverflowWebSocketAuditsBothScopesOnFirstAndSubsequentTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stage := range []string{"first_turn", "subsequent_turn"} {
		for _, blocked := range []int64{10, 20, 0} {
			t.Run(stage+map[int64]string{10: "/source-blocks", 20: "/target-blocks", 0: "/both-allow"}[blocked], func(t *testing.T) {
				sourceID, targetID := int64(10), int64(20)
				key := &service.APIKey{ID: 3, UserID: 2, GroupID: &targetID, Group: &service.Group{ID: targetID}, SubscriptionOverflow: &service.SubscriptionOverflowEvidence{SourceGroupID: sourceID, TargetGroupID: targetID, SourceGroup: &service.Group{ID: sourceID}}}
				engine := &overflowWSGroupAuditEngine{blockedGroup: blocked}
				h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
				c.Set(securityAuditWSTurnContextKey, 2)
				decision := h.checkSecurityAuditStage(c, nil, key, middleware2.AuthSubject{UserID: 2}, service.ContentModerationProtocolOpenAIResponses, "gpt-test", []byte("{\"type\":\"response.create\",\"response\":{\"input\":\"hi\"}}"), stage)
				require.NotNil(t, decision)
				require.Equal(t, blocked == 0, decision.AllowNextStage)
				if blocked == 10 {
					require.Equal(t, []int64{10}, engine.groups)
				} else {
					require.Equal(t, []int64{10, 20}, engine.groups, "a cached source allow must never bypass the target audit")
				}
			})
		}
	}
}

func TestSubscriptionOverflowWebSocketDedupeIsScopedByGroup(t *testing.T) {
	engine := &overflowWSGroupAuditEngine{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte("{\"type\":\"response.create\",\"response\":{\"input\":\"hi\"}}")
	coordinator := securityaudit.NewCoordinator(nil, engine)
	sourceID, targetID := int64(10), int64(20)
	source := &service.APIKey{GroupID: &sourceID}
	target := &service.APIKey{GroupID: &targetID}
	for _, key := range []*service.APIKey{source, source, target, target} {
		decision := runSecurityAudit(c, nil, coordinator, nil, key, middleware2.AuthSubject{}, service.ContentModerationProtocolOpenAIResponses, "gpt-test", payload, "subsequent_turn")
		require.True(t, decision.AllowNextStage)
	}
	require.Equal(t, []int64{10, 20}, engine.groups)
}
