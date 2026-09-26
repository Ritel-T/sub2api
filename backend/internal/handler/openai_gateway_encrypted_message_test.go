//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type encryptedMessageHandlerRepo struct {
	*grokCredentialHandlerRepo
}

func (r *encryptedMessageHandlerRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var accounts []service.Account
	for _, account := range r.accounts {
		for _, platform := range platforms {
			if account.Platform == platform {
				accounts = append(accounts, account)
				break
			}
		}
	}
	return accounts, nil
}

func TestResponsesEncryptedMessageCapabilities_OriginalHTTPBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"agent_message", `{"model":"gpt-6-astra","input":[{"type":"agent_message","content":[{"type":"input_text","text":"test wrapper"},{"type":"encrypted_content","encrypted_content":"synthetic-test-marker"}]}]}`},
		{"shorthand", `{"model":"gpt-6-astra","input":[{"role":"user","content":[{"type":"encrypted_content","encrypted_content":"synthetic-test-marker"}]}]}`},
		{"image_generation", `{"model":"gpt-6-astra","tools":[{"type":"image_generation"}],"input":[{"type":"message","role":"user","content":[{"type":"encrypted_content","encrypted_content":"synthetic-test-marker"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(92000)
			repo := &encryptedMessageHandlerRepo{grokCredentialHandlerRepo: &grokCredentialHandlerRepo{accounts: []service.Account{{
				ID: 92001, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				GroupIDs: []int64{groupID},
				Extra:    map[string]any{"openai_excel_bps": true},
			}}}}
			upstream := &grokCredentialHandlerUpstream{}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			gateway := service.NewOpenAIGatewayService(
				repo, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), nil, billingCache, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
			)
			cache := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
					t.Error("incompatible BPS account must never reach admission")
					return false, nil
				},
			}
			h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
			key := &service.APIKey{
				ID: 92002, GroupID: &groupID,
				User:  &service.User{ID: 92003, Status: service.StatusActive},
				Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowImageGeneration: true},
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID, Concurrency: 1})

			h.Responses(c)

			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Greater(t, repo.selectorCalls(), 0, "the test must reach account selection")
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected, "reject before either BPS or same-account native forwarding")
			require.Empty(t, upstream.accountHits())
			require.NotContains(t, recorder.Body.String(), "synthetic-test-marker")
		})
	}
}
