package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGPT61SolOAuthResponsesStripsUnsupportedCacheParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, officialClient := range []bool{false, true} {
			for _, cache := range []string{`,"prompt_cache_retention":"24h"`, `,"prompt_cache_options":{"ttl":"30m"}`, `,"prompt_cache_retention":"24h","prompt_cache_options":{"ttl":"30m"}`} {
				t.Run(fmt.Sprintf("passthrough=%t/official=%t/%s", passthrough, officialClient, cache), func(t *testing.T) {
					body := []byte(`{"model":"gpt-6.1-sol","input":"hello","stream":true,"reasoning":{"effort":"none"},"temperature":0.7,"top_p":0.9,"top_logprobs":2,"logprobs":true,"include":["reasoning.encrypted_content","message.output_text.logprobs"]` + cache + `}`)
					account := excelAccount()
					account.Extra = map[string]any{"openai_passthrough": passthrough}
					account.Credentials["model_mapping"] = map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}
					wsBody, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
					require.NoError(t, err)
					for _, field := range []string{"prompt_cache_retention", "prompt_cache_options"} {
						require.False(t, gjson.GetBytes(wsBody, field).Exists(), "WS: "+field)
					}
					wire := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r61_oauth\",\"status\":\"completed\",\"model\":\"gpt-6.1-sol\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n"
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewBufferString(wire))}}
					svc := openAIClientToolsTestService(upstream)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					if officialClient {
						c.Request.Header.Set("User-Agent", "codex_cli_rs/0.153.0")
						c.Request.Header.Set("originator", "codex_cli_rs")
					}
					_, err = svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
					require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
					for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs", "prompt_cache_retention", "prompt_cache_options"} {
						require.False(t, gjson.GetBytes(upstream.lastBody, field).Exists(), "HTTP: "+field)
					}
				})
			}
		}
	}
}
