//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardAsAnthropic_ReasoningReplayTargetIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, platform, kind, base, model string
		wantOpenAI                        bool
	}{
		{"official API key", PlatformOpenAI, AccountTypeAPIKey, "https://api.openai.com/v1", "gpt-6-astra", true},
		{"Codex OAuth", PlatformOpenAI, AccountTypeOAuth, "", "gpt-6-astra", true},
		{"Codex setup token", PlatformOpenAI, AccountTypeSetupToken, "", "gpt-6-astra", true},
		{"unverified compatible endpoint", PlatformOpenAI, AccountTypeAPIKey, "http://upstream.example", "gpt-6-astra", false},
		{"xAI with GPT client alias", PlatformGrok, AccountTypeAPIKey, "https://api.x.ai/v1", "gpt-6-astra", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := rawChatCompletionsTestAccount()
			account.Platform, account.Type = tc.platform, tc.kind
			account.Credentials["base_url"] = tc.base
			account.Credentials["access_token"] = "synthetic-token"
			body := []byte(`{"model":"` + tc.model + `","max_tokens":128,"stream":false,"messages":[
				{"role":"user","content":"Inspect"},
				{"role":"assistant","content":[
					{"type":"thinking","thinking":"OpenAI summary","signature":"gAAAA-synthetic-cipher"},
					{"type":"thinking","thinking":"Other provider","signature":"other-provider-cipher"},
					{"type":"thinking","thinking":"Unsigned"},
					{"type":"tool_use","id":"call_read","name":"read","input":{"path":"fixture"}}
				]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_read","content":"fixture result"}]}
			],"tools":[{"name":"read","input_schema":{"type":"object"}}]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_synthetic\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Done\"}]}]}}\n\n")),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			var ciphers []string
			var calls, results []string
			for _, item := range gjson.GetBytes(upstream.lastBody, "input").Array() {
				switch item.Get("type").String() {
				case "reasoning":
					ciphers = append(ciphers, item.Get("encrypted_content").String())
				case "function_call":
					calls = append(calls, item.Get("call_id").String())
				case "function_call_output":
					results = append(results, item.Get("call_id").String())
				}
			}
			if tc.wantOpenAI {
				require.Equal(t, []string{"gAAAA-synthetic-cipher"}, ciphers)
			} else {
				require.Equal(t, []string{"other-provider-cipher"}, ciphers)
			}
			require.Equal(t, []string{"call_read"}, calls)
			require.Equal(t, calls, results)
		})
	}
}
