//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIResponsesLiteTools_MovesNamespacesAndKeepsSupportedTools(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.6-terra",
		"tools": []any{
			map[string]any{"type": "function", "name": "shell"},
			map[string]any{"type": "custom", "name": "exec"},
			map[string]any{"type": "tool_search"},
			map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
				map[string]any{"type": "function", "name": "spawn_agent"},
			}},
		},
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "hello"},
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{
				map[string]any{"type": "namespace", "name": "image_gen"},
				map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
					map[string]any{"type": "function", "name": "spawn_agent"},
				}},
			}},
		},
		"tool_choice": map[string]any{"type": "namespace", "name": "collaboration"},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.NoError(t, err)
	require.True(t, changed)
	tools := reqBody["tools"].([]any)
	require.Len(t, tools, 3)
	require.Equal(t, "function", tools[0].(map[string]any)["type"])
	require.Equal(t, "custom", tools[1].(map[string]any)["type"])
	require.Equal(t, "tool_search", tools[2].(map[string]any)["type"])
	require.Equal(t, false, reqBody["parallel_tool_calls"])
	input := reqBody["input"].([]any)
	require.Len(t, input, 2)
	additional := input[1].(map[string]any)["tools"].([]any)
	require.Len(t, additional, 2)
	require.Equal(t, "image_gen", additional[0].(map[string]any)["name"])
	require.Equal(t, "collaboration", additional[1].(map[string]any)["name"], "existing namespace must not be duplicated")
	require.Equal(t, map[string]any{"type": "namespace", "name": "collaboration"}, reqBody["tool_choice"])
}

func TestNormalizeOpenAIResponsesLiteTools_PreservesDeferredFlagsWithToolSearch(t *testing.T) {
	reqBody := map[string]any{
		"tools": []any{
			map[string]any{"type": "tool_search"},
			map[string]any{"type": "function", "name": "shell", "defer_loading": true},
		},
	}

	_, err := normalizeOpenAIResponsesLiteTools(reqBody)
	require.NoError(t, err)
	tools := reqBody["tools"].([]any)
	require.Equal(t, "tool_search", tools[0].(map[string]any)["type"])
	require.Equal(t, true, tools[1].(map[string]any)["defer_loading"])
}

func TestNormalizeOpenAIResponsesLiteTools_RejectsConflictingAdditionalTool(t *testing.T) {
	reqBody := map[string]any{
		"tools": []any{map[string]any{
			"type":  "namespace",
			"name":  "collaboration",
			"tools": []any{map[string]any{"type": "function", "name": "spawn_agent"}},
		}},
		"input": []any{map[string]any{
			"type": "additional_tools",
			"tools": []any{map[string]any{
				"type":  "namespace",
				"name":  "collaboration",
				"tools": []any{map[string]any{"type": "function", "name": "send_message"}},
			}},
		}},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.ErrorContains(t, err, `conflicts with migrated tool type "namespace" name "collaboration"`)
	require.False(t, changed)
	require.Len(t, reqBody["tools"], 1, "conflicts must not partially remove top-level tools")
}

func TestNormalizeOpenAIResponsesLiteTools_DeduplicatesAcrossAdditionalToolItems(t *testing.T) {
	namespace := map[string]any{
		"type":  "namespace",
		"name":  "collaboration",
		"tools": []any{map[string]any{"type": "function", "name": "spawn_agent"}},
	}
	reqBody := map[string]any{
		"tools": []any{namespace},
		"input": []any{
			map[string]any{
				"type":  "additional_tools",
				"tools": []any{map[string]any{"type": "custom", "name": "exec"}},
			},
			map[string]any{
				"type":  "additional_tools",
				"tools": []any{namespace},
			},
		},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, reqBody, "tools")
	input := reqBody["input"].([]any)
	require.Len(t, input[0].(map[string]any)["tools"], 1)
	require.Len(t, input[1].(map[string]any)["tools"], 1)
}

func TestNormalizeOpenAIResponsesLiteTools_ConvertsStringInput(t *testing.T) {
	reqBody := map[string]any{
		"input": "hello",
		"tools": []any{map[string]any{
			"type": "namespace",
			"name": "collaboration",
		}},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.NotContains(t, reqBody, "tools")
	input := reqBody["input"].([]any)
	require.Len(t, input, 2)
	require.Equal(t, "message", input[0].(map[string]any)["type"])
	require.Equal(t, "hello", input[0].(map[string]any)["content"])
	require.Equal(t, "additional_tools", input[1].(map[string]any)["type"])
}

func TestNormalizeOpenAIResponsesLiteTools_KeepsSupportedTopLevelTools(t *testing.T) {
	reqBody := map[string]any{
		"reasoning": map[string]any{"context": "all_turns"},
		"tools": []any{
			map[string]any{"type": "function", "name": "shell"},
			map[string]any{"type": "custom", "name": "exec"},
			map[string]any{"type": "tool_search"},
			"custom shorthand",
		},
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, reqBody["tools"], 4)
	require.Equal(t, false, reqBody["parallel_tool_calls"])
}

func TestNormalizeOpenAIResponsesLiteTools_ForcesParallelToolCallsFalse(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "top-level tools",
			body: map[string]any{
				"tools":               []any{map[string]any{"type": "function", "name": "shell"}},
				"parallel_tool_calls": true,
			},
		},
		{
			name: "input additional tools",
			body: map[string]any{
				"input": []any{map[string]any{
					"type":  "additional_tools",
					"tools": []any{map[string]any{"type": "namespace", "name": "collaboration"}},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed, err := normalizeOpenAIResponsesLiteTools(tt.body)

			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, false, tt.body["parallel_tool_calls"])
		})
	}
}

func TestNormalizeOpenAIResponsesLiteTools_ForcesParallelToolCallsWithoutTools(t *testing.T) {
	reqBody := map[string]any{
		"reasoning":           map[string]any{"context": "all_turns"},
		"parallel_tool_calls": true,
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, false, reqBody["parallel_tool_calls"])
}

func TestNormalizeOpenAIResponsesLiteTools_CoercesNonBooleanParallelToolCalls(t *testing.T) {
	for _, value := range []any{"false", float64(0), nil, map[string]any{}} {
		reqBody := map[string]any{
			"tools":               []any{map[string]any{"type": "function", "name": "shell"}},
			"parallel_tool_calls": value,
		}

		changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, false, reqBody["parallel_tool_calls"])
	}

	reqBody := map[string]any{"parallel_tool_calls": []any{}}
	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, false, reqBody["parallel_tool_calls"])
}

func TestNormalizeOpenAIResponsesLiteTools_ParallelToolCallsIsIdempotent(t *testing.T) {
	reqBody := map[string]any{
		"reasoning":           map[string]any{"context": "all_turns"},
		"tools":               []any{map[string]any{"type": "function", "name": "shell"}},
		"parallel_tool_calls": true,
	}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, false, reqBody["parallel_tool_calls"])

	changed, err = normalizeOpenAIResponsesLiteTools(reqBody)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, false, reqBody["parallel_tool_calls"])
}

func TestNormalizeOpenAIResponsesLiteTools_EnsuresReasoningContext(t *testing.T) {
	tests := []struct {
		name      string
		reasoning any
	}{
		{name: "missing"},
		{name: "missing context", reasoning: map[string]any{"effort": "high"}},
		{name: "wrong context", reasoning: map[string]any{"effort": "medium", "context": "current_turn"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]any{"input": "hello"}
			if tt.reasoning != nil {
				reqBody["reasoning"] = tt.reasoning
			}

			changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

			require.NoError(t, err)
			require.True(t, changed)
			reasoning := reqBody["reasoning"].(map[string]any)
			require.Equal(t, "all_turns", reasoning["context"])
			if tt.name != "missing" {
				require.Equal(t, tt.reasoning.(map[string]any)["effort"], reasoning["effort"])
			}
		})
	}
}

func TestNormalizeOpenAIResponsesLiteTools_RejectsNonObjectReasoning(t *testing.T) {
	reqBody := map[string]any{"reasoning": "high"}

	changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

	require.ErrorContains(t, err, "reasoning to be an object")
	require.False(t, changed)
	require.Equal(t, "high", reqBody["reasoning"])
}

func TestNormalizeOpenAIResponsesLiteTools_RejectsUnsupportedTools(t *testing.T) {
	tests := []struct {
		name string
		tool map[string]any
		want string
	}{
		{name: "hosted web search", tool: map[string]any{"type": "web_search"}, want: `top-level tool type "web_search"`},
		{name: "hosted image generation", tool: map[string]any{"type": "image_generation"}, want: `top-level tool type "image_generation"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]any{"tools": []any{tt.tool}}
			changed, err := normalizeOpenAIResponsesLiteTools(reqBody)
			require.ErrorContains(t, err, tt.want)
			require.False(t, changed)
			require.Len(t, reqBody["tools"], 1, "validation errors must not partially mutate tools")
		})
	}
}

func TestNormalizeOpenAIResponsesLiteToolsPayload_PreservesResponseCreateShape(t *testing.T) {
	body := []byte(`{
		"type":"response.create",
		"model":"gpt-5.6-terra",
		"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"},
		"input":[{"type":"message","role":"user","content":"hello"}],
		"tools":[{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent"}]}],
		"tool_choice":{"type":"namespace","name":"collaboration"}
	}`)

	updated, changed, err := normalizeOpenAIResponsesLiteToolsPayload(body)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "response.create", gjson.GetBytes(updated, "type").String())
	require.False(t, gjson.GetBytes(updated, "tools").Exists())
	require.Equal(t, "collaboration", gjson.GetBytes(updated, `input.#(type=="additional_tools").tools.0.name`).String())
	require.Equal(t, "namespace", gjson.GetBytes(updated, "tool_choice.type").String())
	require.True(t, gjson.GetBytes(updated, "parallel_tool_calls").Exists())
	require.False(t, gjson.GetBytes(updated, "parallel_tool_calls").Bool())
}

func TestNormalizeOpenAIResponsesLiteParallelToolCalls(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantChanged bool
	}{
		{name: "missing", body: `{"reasoning":{"context":"all_turns"}}`, wantChanged: true},
		{name: "true", body: `{"reasoning":{"context":"all_turns"},"parallel_tool_calls":true}`, wantChanged: true},
		{name: "already false", body: `{"reasoning":{"context":"all_turns"},"parallel_tool_calls":false}`, wantChanged: false},
		{name: "null", body: `{"reasoning":{"context":"all_turns"},"parallel_tool_calls":null}`, wantChanged: true},
		{name: "non boolean", body: `{"reasoning":{"context":"all_turns"},"parallel_tool_calls":"false"}`, wantChanged: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody map[string]any
			require.NoError(t, json.Unmarshal([]byte(tt.body), &reqBody))

			changed, err := normalizeOpenAIResponsesLiteTools(reqBody)

			require.NoError(t, err)
			require.Equal(t, tt.wantChanged, changed)
			parallel, ok := reqBody["parallel_tool_calls"].(bool)
			require.True(t, ok)
			require.False(t, parallel)

			normalized, rawChanged, rawErr := normalizeOpenAIResponsesLiteParallelToolCallsPayload([]byte(tt.body))
			require.NoError(t, rawErr)
			require.Equal(t, tt.wantChanged, rawChanged)
			require.True(t, gjson.GetBytes(normalized, "parallel_tool_calls").Exists())
			require.False(t, gjson.GetBytes(normalized, "parallel_tool_calls").Bool())
		})
	}
}

func TestNormalizeOpenAIResponsesLiteParallelToolCallsPayloadCoercesMalformedValues(t *testing.T) {
	for _, body := range []string{
		`{"parallel_tool_calls":null}`,
		`{"parallel_tool_calls":"false"}`,
	} {
		normalized, changed, err := normalizeOpenAIResponsesLiteParallelToolCallsPayload([]byte(body))
		require.NoError(t, err)
		require.True(t, changed)
		require.True(t, gjson.GetBytes(normalized, "parallel_tool_calls").Exists())
		require.False(t, gjson.GetBytes(normalized, "parallel_tool_calls").Bool())
	}
}

func TestOpenAIResponsesLiteWebSocketRequestHonorsHeaderOverride(t *testing.T) {
	litePayload := []byte(`{"type":"response.create","client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}}`)
	nonLitePayload := []byte(`{"type":"response.create"}`)
	newAccount := func(override string) *Account {
		credentials := map[string]any{"api_key": "sk-test"}
		if override != "" {
			credentials[credKeyHeaderOverrideEnabled] = true
			credentials[credKeyHeaderOverrides] = map[string]any{responsesLiteHeaderKey: override}
		}
		return &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: credentials}
	}

	require.True(t, isOpenAIResponsesLiteWebSocketRequest("", litePayload, newAccount("")))
	require.True(t, isOpenAIResponsesLiteWebSocketRequest("true", nonLitePayload, newAccount("")))
	require.False(t, isOpenAIResponsesLiteWebSocketRequest("true", litePayload, newAccount("false")))
	require.True(t, isOpenAIResponsesLiteWebSocketRequest("", nonLitePayload, newAccount("true")))
	require.False(t, isOpenAIResponsesLiteWebSocketRequest("true", litePayload, &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}))
	legacyImplicitOAuth := &Account{Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "oauth-token"}}
	require.True(t, isOpenAIResponsesLiteOutboundRequest("true", legacyImplicitOAuth))
	require.True(t, isOpenAIResponsesLiteWebSocketRequest("", litePayload, legacyImplicitOAuth))

	falseOverride := newAccount("false")
	falseHeaders := http.Header{responsesLiteHeaderKey: []string{"false"}}
	applyOpenAIResponsesLiteWebSocketHTTPHeader(falseHeaders, "true", litePayload, falseOverride)
	require.Equal(t, "false", openAIResponsesLiteHeaderValue(falseHeaders))

	trueOverride := newAccount("true")
	trueHeaders := http.Header{responsesLiteHeaderKey: []string{"true"}}
	applyOpenAIResponsesLiteWebSocketHTTPHeader(trueHeaders, "", nonLitePayload, trueOverride)
	require.Equal(t, "true", openAIResponsesLiteHeaderValue(trueHeaders))
	equalFoldMatches := 0
	for name := range trueHeaders {
		if strings.EqualFold(name, responsesLiteHeader) {
			equalFoldMatches++
		}
	}
	require.Equal(t, 1, equalFoldMatches)
}

func TestApplyCodexOAuthTransform_PreservesLiteNamespaceToolChoice(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.6-terra",
		"input": []any{map[string]any{
			"type": "additional_tools",
			"tools": []any{map[string]any{
				"type": "namespace",
				"name": "collaboration",
			}},
		}},
		"tool_choice": map[string]any{"type": "namespace", "name": "collaboration"},
	}

	applyCodexOAuthTransform(reqBody, true, false)

	require.Equal(t, map[string]any{"type": "namespace", "name": "collaboration"}, reqBody["tool_choice"])
}

func TestOpenAIGatewayServiceForward_NormalizesResponsesLiteToolsForOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, passthrough := range []bool{false, true} {
		name := "managed"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
			c.Request.Header.Set(responsesLiteHeader, "true")
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_lite\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
						"data: [DONE]\n\n",
				)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{
				ID: 501, Name: "responses-lite", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Concurrency: 1, Status: StatusActive, Schedulable: true, RateMultiplier: f64p(1),
				Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
				Extra:       map[string]any{"openai_passthrough": passthrough},
			}
			body := []byte(`{
				"model":"gpt-5.6-terra","stream":true,"instructions":"test",
				"reasoning":{"effort":"high","context":"current_turn"},
				"parallel_tool_calls":true,
				"tools":[
					{"type":"function","name":"shell","parameters":{"type":"object"}},
					{"type":"custom","name":"exec"},
					{"type":"tool_search"},
					{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object"}}]}
				],
				"input":[{"type":"message","role":"user","content":"hello"}],
				"tool_choice":{"type":"namespace","name":"collaboration"}
			}`)

			result, err := svc.Forward(context.Background(), c, account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "true", upstream.lastReq.Header.Get(responsesLiteHeader))
			require.True(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Exists())
			require.False(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Bool())
			require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
			require.Equal(t, "all_turns", gjson.GetBytes(upstream.lastBody, "reasoning.context").String())
			require.False(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="namespace")`).Exists())
			require.Equal(t, "shell", gjson.GetBytes(upstream.lastBody, `tools.#(type=="function").name`).String())
			require.Equal(t, "exec", gjson.GetBytes(upstream.lastBody, `tools.#(type=="custom").name`).String())
			require.True(t, gjson.GetBytes(upstream.lastBody, `tools.#(type=="tool_search")`).Exists())
			require.Equal(t, "collaboration", gjson.GetBytes(upstream.lastBody, `input.#(type=="additional_tools").tools.0.name`).String())
			require.Equal(t, "namespace", gjson.GetBytes(upstream.lastBody, "tool_choice.type").String())
			require.Equal(t, "collaboration", gjson.GetBytes(upstream.lastBody, "tool_choice.name").String())
			require.True(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Exists())
			require.False(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Bool())

			coerceRec := httptest.NewRecorder()
			coerceCtx, _ := gin.CreateTestContext(coerceRec)
			coerceCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
			coerceCtx.Request.Header.Set(responsesLiteHeader, "true")
			coerceUpstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_lite_coerced\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
						"data: [DONE]\n\n",
				)),
			}}
			svc.httpUpstream = coerceUpstream

			result, err = svc.Forward(context.Background(), coerceCtx, account, []byte(`{"model":"gpt-5.6-terra","stream":true,"tools":[{"type":"function","name":"shell"}],"parallel_tool_calls":"false"}`))

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, coerceUpstream.lastReq)
			require.True(t, gjson.GetBytes(coerceUpstream.lastBody, "parallel_tool_calls").Exists())
			require.False(t, gjson.GetBytes(coerceUpstream.lastBody, "parallel_tool_calls").Bool())

			for _, malformed := range []struct {
				body      string
				wantParam string
			}{
				{body: `{"model":"gpt-5.6-terra","tools":{}}`, wantParam: "tools"},
				{body: `{"model":"gpt-5.6-terra","reasoning":[]}`, wantParam: "reasoning"},
			} {
				rec := httptest.NewRecorder()
				requestCtx, _ := gin.CreateTestContext(rec)
				requestCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
				requestCtx.Request.Header.Set(responsesLiteHeader, "true")

				result, err = svc.Forward(context.Background(), requestCtx, account, []byte(malformed.body))

				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, malformed.wantParam, gjson.Get(rec.Body.String(), "error.param").String())
			}
		})
	}
}

func TestBuildUpstreamRequest_GuardsResponsesLiteForLegacyImplicitOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
	c.Request.Header.Set(responsesLiteHeader, "true")
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{
		ID: 503, Name: "legacy-implicit-openai-oauth", Type: AccountTypeOAuth,
		Concurrency: 1, Status: StatusActive, Schedulable: true, RateMultiplier: f64p(1),
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
	}
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"instructions":"test","parallel_tool_calls":true,"input":"hello"}`)
	normalized, changed, err := normalizeOpenAIResponsesLitePayloadForAccount(body, account)
	require.NoError(t, err)
	require.True(t, changed)

	upstreamReq, err := svc.buildUpstreamRequest(context.Background(), c, account, normalized, "oauth-token", true, "", false)

	require.NoError(t, err)
	require.NotNil(t, upstreamReq)
	upstreamBody, readErr := io.ReadAll(upstreamReq.Body)
	require.NoError(t, readErr)
	require.Equal(t, "true", openAIResponsesLiteHeaderValue(upstreamReq.Header))
	require.True(t, gjson.GetBytes(upstreamBody, "parallel_tool_calls").Exists())
	require.False(t, gjson.GetBytes(upstreamBody, "parallel_tool_calls").Bool())
	require.Equal(t, "all_turns", gjson.GetBytes(upstreamBody, "reasoning.context").String())
}

func TestOpenAIGatewayServiceForward_GuardsResponsesLiteForAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		passthrough    bool
		clientLite     bool
		headerOverride string
		wantParallel   bool
		body           string
	}{
		{name: "managed client header", clientLite: true},
		{name: "passthrough client header", passthrough: true, clientLite: true},
		{
			name:       "managed client header without top level tools",
			clientLite: true,
			body:       `{"model":"gpt-5.6-sol","stream":true,"instructions":"test","parallel_tool_calls":true,"input":"hello"}`,
		},
		{
			name:        "passthrough client header with additional tools only",
			passthrough: true,
			clientLite:  true,
			body:        `{"model":"gpt-5.6-sol","stream":true,"instructions":"test","parallel_tool_calls":true,"input":[{"type":"additional_tools","tools":[{"type":"function","name":"lookup"}]}]}`,
		},
		{name: "managed account header override", headerOverride: "true"},
		{name: "passthrough account header override", passthrough: true, headerOverride: "true"},
		{name: "account override disables client lite", clientLite: true, headerOverride: "false", wantParallel: true},
		{name: "managed non lite", wantParallel: true},
		{name: "passthrough non lite", passthrough: true, wantParallel: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.149.0")
			if tt.clientLite {
				c.Request.Header.Set(responsesLiteHeader, "true")
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_lite_api_key\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n" +
						"data: [DONE]\n\n",
				)),
			}}
			credentials := map[string]any{"api_key": "sk-test"}
			if tt.headerOverride != "" {
				credentials[credKeyHeaderOverrideEnabled] = true
				credentials[credKeyHeaderOverrides] = map[string]any{responsesLiteHeaderKey: tt.headerOverride}
			}
			account := &Account{
				ID: 502, Name: "responses-lite-api-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Concurrency: 1, Status: StatusActive, Schedulable: true, RateMultiplier: f64p(1),
				Credentials: credentials,
				Extra:       map[string]any{"openai_passthrough": tt.passthrough},
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			body := []byte(tt.body)
			if len(body) == 0 {
				body = []byte(`{
					"model":"gpt-5.6-sol","stream":true,"instructions":"test",
					"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
					"parallel_tool_calls":true,
					"input":[{"type":"message","role":"user","content":"hello"}]
				}`)
			}

			result, err := svc.Forward(context.Background(), c, account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			effectiveLite := tt.headerOverride == "true" || (tt.headerOverride == "" && tt.clientLite)
			if effectiveLite {
				require.Equal(t, "true", openAIResponsesLiteHeaderValue(upstream.lastReq.Header))
				require.True(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Exists())
				require.False(t, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Bool())
			} else {
				if tt.headerOverride == "false" {
					require.Equal(t, "false", openAIResponsesLiteHeaderValue(upstream.lastReq.Header))
				} else {
					require.Empty(t, openAIResponsesLiteHeaderValue(upstream.lastReq.Header))
				}
				require.Equal(t, tt.wantParallel, gjson.GetBytes(upstream.lastBody, "parallel_tool_calls").Bool())
			}
		})
	}
}
