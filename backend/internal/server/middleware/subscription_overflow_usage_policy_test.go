package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func overflowUsagePolicyKey(source, target *service.Group) *service.APIKey {
	return &service.APIKey{ID: 11, Group: target, SubscriptionOverflow: &service.SubscriptionOverflowEvidence{SourceGroup: source}}
}

func TestSubscriptionOverflowUsagePolicyIntersectsBothGroups(t *testing.T) {
	for _, scope := range []string{"source", "target"} {
		for _, policy := range []string{"codex", "subagents"} {
			t.Run(scope+"/"+policy, func(t *testing.T) {
				source, target := &service.Group{ID: 1}, &service.Group{ID: 2}
				restricted := source
				if scope == "target" {
					restricted = target
				}
				restricted.CodexCLIOnly = policy == "codex"
				restricted.BlockSubagents = policy == "subagents"
				key := overflowUsagePolicyKey(source, target)
				body := `{"model":"gpt-test","client_metadata":{"x-openai-subagent":"worker"}}`
				r := gin.New()
				r.POST("/v1/responses", func(c *gin.Context) {
					err := ValidateSubscriptionOverflowRequestPolicy(c, key, []byte(body))
					expected := "CODEX_CLIENT_REQUIRED"
					if policy == "subagents" {
						expected = "SUBAGENTS_BLOCKED"
					}
					require.Equal(t, expected, infraerrors.Reason(err))
					require.Equal(t, http.StatusForbidden, infraerrors.Code(err))
					require.False(t, c.Writer.Written(), "protocol handler owns the response")
					reason, marked := GetIngressRejectReason(c)
					require.True(t, marked)
					if policy == "subagents" {
						require.Equal(t, IngressRejectSubagentsBlocked, reason)
					} else {
						require.Equal(t, IngressRejectCodexClientRequired, reason)
					}
				})
				r.ServeHTTP(httptest.NewRecorder(), codexGroupRequest(http.MethodPost, "/v1/responses", body, false))
				require.Same(t, target, key.Group, "admission must not change billing projection")
				require.Equal(t, scope == "source" && policy == "codex", source.CodexCLIOnly)
				require.Equal(t, scope == "target" && policy == "codex", target.CodexCLIOnly)
			})
		}
	}
}

func TestSubscriptionOverflowUsageMiddlewareEnforcesBothScopes(t *testing.T) {
	body := `{"model":"gpt-test","stream":true}`
	for _, scope := range []string{"source", "target"} {
		for _, official := range []bool{false, true} {
			t.Run(scope+"/"+map[bool]string{true: "official", false: "other"}[official], func(t *testing.T) {
				source, target := &service.Group{}, &service.Group{}
				if scope == "source" {
					source.CodexCLIOnly = true
				} else {
					target.CodexCLIOnly = true
				}
				key := overflowUsagePolicyKey(source, target)
				r := gin.New()
				r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key) })
				r.Use(GroupCodexCLIOnly(nil), GroupBlockSubagents())
				r.POST("/v1/responses", func(c *gin.Context) {
					read, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.Equal(t, body, string(read))
					c.Status(http.StatusNoContent)
				})
				req := codexGroupRequest(http.MethodPost, "/v1/responses", body, official)
				req.Header.Set("X-Admin-Test", "true")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if official {
					require.Equal(t, http.StatusNoContent, w.Code)
				} else {
					require.Equal(t, http.StatusForbidden, w.Code)
					require.Contains(t, w.Body.String(), "codex_client_required")
				}
			})
		}
	}
}

func TestSubscriptionOverflowWSChecksBothScopesAcrossTurnsAndRouting(t *testing.T) {
	for _, scope := range []string{"source", "target"} {
		t.Run(scope, func(t *testing.T) {
			source, target := &service.Group{}, &service.Group{}
			restricted := source
			if scope == "target" {
				restricted = target
			}
			restricted.CodexCLIOnly, restricted.BlockSubagents = true, true
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), overflowUsagePolicyKey(source, target)) })
			r.Use(GroupCodexCLIOnly(nil), GroupBlockSubagents())
			r.GET("/v1/responses", func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{Group: &service.Group{}})
				c.Request.Header.Set("X-OpenAI-Subagent", "upstream-generated")
				restricted.CodexCLIOnly, restricted.BlockSubagents = false, false
				for turn := 0; turn < 2; turn++ {
					main := []byte(`{"type":"response.create","model":"gpt-test"}`)
					result := CheckGroupCodexClientFrame(c, main)
					require.True(t, result.Enabled)
					require.True(t, result.Matched)
					require.False(t, CheckGroupSubagentFrame(c, main))
					require.True(t, CheckGroupSubagentFrame(c, []byte(`{"type":"response.create","response":{"model":"gpt-test","client_metadata":{"x-openai-subagent":"worker"}}}`)))
				}
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, codexGroupRequest(http.MethodGet, "/v1/responses", "", true))
			require.Equal(t, http.StatusNoContent, w.Code)
		})
	}
}

func TestSubscriptionOverflowFreshTargetUsesIngressIdentity(t *testing.T) {
	for _, tc := range []struct {
		name            string
		codex, official bool
		marker          bool
		reject          bool
	}{
		{name: "normal client cannot use added upstream identity", codex: true, reject: true},
		{name: "official client survives upstream mutation", codex: true, official: true},
		{name: "upstream subagent marker is ignored", official: true},
		{name: "ingress subagent marker survives stripping", official: true, marker: true, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fallbackID := int64(2)
			initial := &service.APIKey{Group: &service.Group{SubscriptionFallbackGroupID: &fallbackID}}
			source, target := &service.Group{}, &service.Group{CodexCLIOnly: tc.codex, BlockSubagents: !tc.codex}
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), initial) })
			r.Use(GroupCodexCLIOnly(nil), GroupBlockSubagents())
			r.POST("/v1/responses", func(c *gin.Context) {
				c.Request.Header.Set("User-Agent", "codex-tui/0.163.0 (Ubuntu 22.4.0; x86_64) xterm-256color")
				c.Request.Header.Set("Originator", "codex-tui")
				c.Request.Header.Set("X-Codex-Window-Id", "upstream-generated")
				if tc.marker {
					c.Request.Header.Del("X-OpenAI-Subagent")
				} else {
					c.Request.Header.Set("X-OpenAI-Subagent", "upstream-generated")
				}
				err := ValidateSubscriptionOverflowRequestPolicy(c, overflowUsagePolicyKey(source, target), []byte(`{"model":"gpt-test"}`))
				if tc.reject {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.False(t, c.Writer.Written())
			})
			req := codexGroupRequest(http.MethodPost, "/v1/responses", `{"model":"gpt-test"}`, tc.official)
			if tc.marker {
				req.Header.Set("X-OpenAI-Subagent", "worker")
			}
			r.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
