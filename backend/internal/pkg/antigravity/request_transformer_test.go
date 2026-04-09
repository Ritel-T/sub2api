package antigravity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildParts_ThinkingBlockWithoutSignature 测试thinking block无signature时的处理
func TestBuildParts_ThinkingBlockWithoutSignature(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		allowDummyThought bool
		expectedParts     int
		description       string
	}{
		{
			name: "Claude model - downgrade thinking to text without signature",
			content: `[
				{"type": "text", "text": "Hello"},
				{"type": "thinking", "thinking": "Let me think...", "signature": ""},
				{"type": "text", "text": "World"}
			]`,
			allowDummyThought: false,
			expectedParts:     3, // thinking 内容降级为普通 text part
			description:       "Claude模型缺少signature时应将thinking降级为text，并在上层禁用thinking mode",
		},
		{
			name: "Claude model - preserve thinking block with signature",
			content: `[
				{"type": "text", "text": "Hello"},
				{"type": "thinking", "thinking": "Let me think...", "signature": "sig_real_123"},
				{"type": "text", "text": "World"}
			]`,
			allowDummyThought: false,
			expectedParts:     3,
			description:       "Claude模型应透传带 signature 的 thinking block（用于 Vertex 签名链路）",
		},
		{
			name: "Gemini model - use dummy signature",
			content: `[
				{"type": "text", "text": "Hello"},
				{"type": "thinking", "thinking": "Let me think...", "signature": ""},
				{"type": "text", "text": "World"}
			]`,
			allowDummyThought: true,
			expectedParts:     3, // 三个block都保留，thinking使用dummy signature
			description:       "Gemini模型应该为无signature的thinking block使用dummy signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolIDToName := make(map[string]string)
			parts, _, err := buildParts(json.RawMessage(tt.content), toolIDToName, tt.allowDummyThought)

			if err != nil {
				t.Fatalf("buildParts() error = %v", err)
			}

			if len(parts) != tt.expectedParts {
				t.Errorf("%s: got %d parts, want %d parts", tt.description, len(parts), tt.expectedParts)
			}

			switch tt.name {
			case "Claude model - preserve thinking block with signature":
				if len(parts) != 3 {
					t.Fatalf("expected 3 parts, got %d", len(parts))
				}
				if !parts[1].Thought || parts[1].ThoughtSignature != "sig_real_123" {
					t.Fatalf("expected thought part with signature sig_real_123, got thought=%v signature=%q",
						parts[1].Thought, parts[1].ThoughtSignature)
				}
			case "Claude model - downgrade thinking to text without signature":
				if len(parts) != 3 {
					t.Fatalf("expected 3 parts, got %d", len(parts))
				}
				if parts[1].Thought {
					t.Fatalf("expected downgraded text part, got thought=%v signature=%q",
						parts[1].Thought, parts[1].ThoughtSignature)
				}
				if parts[1].Text != "Let me think..." {
					t.Fatalf("expected downgraded text %q, got %q", "Let me think...", parts[1].Text)
				}
			case "Gemini model - use dummy signature":
				if len(parts) != 3 {
					t.Fatalf("expected 3 parts, got %d", len(parts))
				}
				if !parts[1].Thought || parts[1].ThoughtSignature != DummyThoughtSignature {
					t.Fatalf("expected dummy thought signature, got thought=%v signature=%q",
						parts[1].Thought, parts[1].ThoughtSignature)
				}
			}
		})
	}
}

func TestBuildParts_ToolUseSignatureHandling(t *testing.T) {
	content := `[
		{"type": "tool_use", "id": "t1", "name": "Bash", "input": {"command": "ls"}, "signature": "sig_tool_abc"}
	]`

	t.Run("Gemini preserves provided tool_use signature", func(t *testing.T) {
		toolIDToName := make(map[string]string)
		parts, _, err := buildParts(json.RawMessage(content), toolIDToName, true)
		if err != nil {
			t.Fatalf("buildParts() error = %v", err)
		}
		if len(parts) != 1 || parts[0].FunctionCall == nil {
			t.Fatalf("expected 1 functionCall part, got %+v", parts)
		}
		if parts[0].ThoughtSignature != "sig_tool_abc" {
			t.Fatalf("expected preserved tool signature %q, got %q", "sig_tool_abc", parts[0].ThoughtSignature)
		}
	})

	t.Run("Gemini falls back to dummy tool_use signature when missing", func(t *testing.T) {
		contentNoSig := `[
			{"type": "tool_use", "id": "t1", "name": "Bash", "input": {"command": "ls"}}
		]`
		toolIDToName := make(map[string]string)
		parts, _, err := buildParts(json.RawMessage(contentNoSig), toolIDToName, true)
		if err != nil {
			t.Fatalf("buildParts() error = %v", err)
		}
		if len(parts) != 1 || parts[0].FunctionCall == nil {
			t.Fatalf("expected 1 functionCall part, got %+v", parts)
		}
		if parts[0].ThoughtSignature != DummyThoughtSignature {
			t.Fatalf("expected dummy tool signature %q, got %q", DummyThoughtSignature, parts[0].ThoughtSignature)
		}
	})

	t.Run("Claude model - preserve valid signature for tool_use", func(t *testing.T) {
		toolIDToName := make(map[string]string)
		parts, _, err := buildParts(json.RawMessage(content), toolIDToName, false)
		if err != nil {
			t.Fatalf("buildParts() error = %v", err)
		}
		if len(parts) != 1 || parts[0].FunctionCall == nil {
			t.Fatalf("expected 1 functionCall part, got %+v", parts)
		}
		// Claude 模型应透传有效的 signature（Vertex/Google 需要完整签名链路）
		if parts[0].ThoughtSignature != "sig_tool_abc" {
			t.Fatalf("expected preserved tool signature %q, got %q", "sig_tool_abc", parts[0].ThoughtSignature)
		}
	})
}

// TestBuildTools_CustomTypeTools 测试custom类型工具转换
func TestBuildTools_CustomTypeTools(t *testing.T) {
	tests := []struct {
		name        string
		tools       []ClaudeTool
		expectedLen int
		description string
	}{
		{
			name: "Standard tool format",
			tools: []ClaudeTool{
				{
					Name:        "get_weather",
					Description: "Get weather information",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{"type": "string"},
						},
					},
				},
			},
			expectedLen: 1,
			description: "标准工具格式应该正常转换",
		},
		{
			name: "Custom type tool (MCP format)",
			tools: []ClaudeTool{
				{
					Type: "custom",
					Name: "mcp_tool",
					Custom: &ClaudeCustomToolSpec{
						Description: "MCP tool description",
						InputSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"param": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
			expectedLen: 1,
			description: "Custom类型工具应该从Custom字段读取description和input_schema",
		},
		{
			name: "Mixed standard and custom tools",
			tools: []ClaudeTool{
				{
					Name:        "standard_tool",
					Description: "Standard tool",
					InputSchema: map[string]any{"type": "object"},
				},
				{
					Type: "custom",
					Name: "custom_tool",
					Custom: &ClaudeCustomToolSpec{
						Description: "Custom tool",
						InputSchema: map[string]any{"type": "object"},
					},
				},
			},
			expectedLen: 1, // 返回一个GeminiToolDeclaration，包含2个function declarations
			description: "混合标准和custom工具应该都能正确转换",
		},
		{
			name: "Invalid custom tool - nil Custom field",
			tools: []ClaudeTool{
				{
					Type: "custom",
					Name: "invalid_custom",
					// Custom 为 nil
				},
			},
			expectedLen: 0, // 应该被跳过
			description: "Custom字段为nil的custom工具应该被跳过",
		},
		{
			name: "Invalid custom tool - nil InputSchema",
			tools: []ClaudeTool{
				{
					Type: "custom",
					Name: "invalid_custom",
					Custom: &ClaudeCustomToolSpec{
						Description: "Invalid",
						// InputSchema 为 nil
					},
				},
			},
			expectedLen: 0, // 应该被跳过
			description: "InputSchema为nil的custom工具应该被跳过",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildTools(tt.tools)

			if len(result) != tt.expectedLen {
				t.Errorf("%s: got %d tool declarations, want %d", tt.description, len(result), tt.expectedLen)
			}

			// 验证function declarations存在
			if len(result) > 0 && result[0].FunctionDeclarations != nil {
				if len(result[0].FunctionDeclarations) != len(tt.tools) {
					t.Errorf("%s: got %d function declarations, want %d",
						tt.description, len(result[0].FunctionDeclarations), len(tt.tools))
				}
			}
		})
	}
}

func TestBuildTools_PreservesWebSearchAlongsideFunctions(t *testing.T) {
	tools := []ClaudeTool{
		{
			Name:        "get_weather",
			Description: "Get weather information",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Type: "web_search_20250305",
			Name: "web_search",
		},
	}

	result := buildTools(tools)
	require.Len(t, result, 2)
	require.Len(t, result[0].FunctionDeclarations, 1)
	require.Equal(t, "get_weather", result[0].FunctionDeclarations[0].Name)
	require.NotNil(t, result[1].GoogleSearch)
	require.NotNil(t, result[1].GoogleSearch.EnhancedContent)
	require.NotNil(t, result[1].GoogleSearch.EnhancedContent.ImageSearch)
	require.Equal(t, 5, result[1].GoogleSearch.EnhancedContent.ImageSearch.MaxResultCount)
}

func TestBuildGenerationConfig_ThinkingDynamicBudget(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		thinking    *ThinkingConfig
		wantBudget  int
		wantPresent bool
	}{
		{
			name:        "enabled without budget defaults to dynamic (-1)",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "enabled"},
			wantBudget:  -1,
			wantPresent: true,
		},
		{
			name:        "enabled with budget uses the provided value",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "enabled", BudgetTokens: 1024},
			wantBudget:  1024,
			wantPresent: true,
		},
		{
			name:        "enabled with -1 budget uses dynamic (-1)",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "enabled", BudgetTokens: -1},
			wantBudget:  -1,
			wantPresent: true,
		},
		{
			name:        "adaptive on opus4.6 maps to high budget (24576)",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "adaptive", BudgetTokens: 20000},
			wantBudget:  ClaudeAdaptiveHighThinkingBudgetTokens,
			wantPresent: true,
		},
		{
			name:        "adaptive on non-opus model keeps default dynamic (-1)",
			model:       "claude-sonnet-4-5-thinking",
			thinking:    &ThinkingConfig{Type: "adaptive"},
			wantBudget:  -1,
			wantPresent: true,
		},
		{
			name:        "disabled does not emit thinkingConfig",
			model:       "claude-opus-4-6-thinking",
			thinking:    &ThinkingConfig{Type: "disabled", BudgetTokens: 1024},
			wantBudget:  0,
			wantPresent: false,
		},
		{
			name:        "nil thinking does not emit thinkingConfig",
			model:       "claude-opus-4-6-thinking",
			thinking:    nil,
			wantBudget:  0,
			wantPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ClaudeRequest{
				Model:    tt.model,
				Thinking: tt.thinking,
			}
			cfg := buildGenerationConfig(req)
			if cfg == nil {
				t.Fatalf("expected non-nil generationConfig")
			}

			if tt.wantPresent {
				if cfg.ThinkingConfig == nil {
					t.Fatalf("expected thinkingConfig to be present")
				}
				if !cfg.ThinkingConfig.IncludeThoughts {
					t.Fatalf("expected includeThoughts=true")
				}
				if cfg.ThinkingConfig.ThinkingBudget != tt.wantBudget {
					t.Fatalf("expected thinkingBudget=%d, got %d", tt.wantBudget, cfg.ThinkingConfig.ThinkingBudget)
				}
				return
			}

			if cfg.ThinkingConfig != nil {
				t.Fatalf("expected thinkingConfig to be nil, got %+v", cfg.ThinkingConfig)
			}
		})
	}
}

func TestTransformClaudeToGeminiWithOptions_PreservesBillingHeaderSystemBlock(t *testing.T) {
	tests := []struct {
		name   string
		system json.RawMessage
	}{
		{
			name:   "system array",
			system: json.RawMessage(`[{"type":"text","text":"x-anthropic-billing-header keep"}]`),
		},
		{
			name:   "system string",
			system: json.RawMessage(`"x-anthropic-billing-header keep"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claudeReq := &ClaudeRequest{
				Model:  "claude-3-5-sonnet-latest",
				System: tt.system,
				Messages: []ClaudeMessage{
					{
						Role:    "user",
						Content: json.RawMessage(`[{"type":"text","text":"hello"}]`),
					},
				},
			}

			body, err := TransformClaudeToGeminiWithOptions(claudeReq, "project-1", "gemini-2.5-flash", DefaultTransformOptions())
			require.NoError(t, err)

			var req V1InternalRequest
			require.NoError(t, json.Unmarshal(body, &req))
			require.NotNil(t, req.Request.SystemInstruction)

			found := false
			for _, part := range req.Request.SystemInstruction.Parts {
				if strings.Contains(part.Text, "x-anthropic-billing-header keep") {
					found = true
					break
				}
			}

			require.True(t, found, "转换后的 systemInstruction 应保留 x-anthropic-billing-header 内容")
		})
	}
}

func TestBuildSystemInstruction_OfficialFormat_NoUserSystem(t *testing.T) {
	instruction := buildSystemInstruction(nil, "claude-sonnet-4-5", DefaultTransformOptions(), nil)
	require.NotNil(t, instruction)
	require.Equal(t, "user", instruction.Role)
	require.Len(t, instruction.Parts, 2)
	require.Equal(t, antigravityIdentity, instruction.Parts[0].Text)
	require.Contains(t, instruction.Parts[1].Text, "[ignore]")
	require.NotContains(t, instruction.Parts[1].Text, "SYSTEM_PROMPT_END")
	require.NotContains(t, instruction.Parts[1].Text, "internal initialization logs")
}

func TestBuildSystemInstruction_OfficialFormat_WithUserSystem(t *testing.T) {
	system := json.RawMessage(`"Follow user instructions carefully."`)
	instruction := buildSystemInstruction(system, "claude-sonnet-4-5", DefaultTransformOptions(), nil)
	require.NotNil(t, instruction)
	require.Len(t, instruction.Parts, 3)
	require.Equal(t, antigravityIdentity, instruction.Parts[0].Text)
	require.Contains(t, instruction.Parts[1].Text, "[ignore]")
	require.Equal(t, "Follow user instructions carefully.", instruction.Parts[2].Text)
	require.NotContains(t, instruction.Parts[1].Text, "internal initialization logs")
}

func TestBuildSystemInstruction_OfficialFormat_ExistingIdentity(t *testing.T) {
	system := json.RawMessage(`"You are Antigravity already. Keep this."`)
	instruction := buildSystemInstruction(system, "claude-sonnet-4-5", DefaultTransformOptions(), nil)
	require.NotNil(t, instruction)
	require.Len(t, instruction.Parts, 1)
	require.Equal(t, "You are Antigravity already. Keep this.", instruction.Parts[0].Text)
}

func TestGenerateSessionID_Format(t *testing.T) {
	contents := []GeminiContent{{
		Role:  "user",
		Parts: []GeminiPart{{Text: "hello world"}},
	}}
	id := generateStableSessionID(contents)
	require.Regexp(t, `^[0-9a-fA-F\-]{36}[0-9]{13}$`, id)
}

func TestStopSequences_NotSentByDefault(t *testing.T) {
	req := &ClaudeRequest{Model: "claude-sonnet-4-5", MaxTokens: 10}
	cfg := buildGenerationConfig(req)
	require.NotNil(t, cfg)
	require.Nil(t, cfg.StopSequences)
}

func TestToolConfig_NotSentWithoutTools(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []ClaudeMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}
	body, err := TransformClaudeToGeminiWithOptions(claudeReq, "project-1", "claude-sonnet-4-5", DefaultTransformOptions())
	require.NoError(t, err)

	var req V1InternalRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Nil(t, req.Request.ToolConfig)
}

func TestTransformClaudeToGeminiWithOptions_PreservesWebSearchAlongsideFunctions(t *testing.T) {
	claudeReq := &ClaudeRequest{
		Model: "claude-3-5-sonnet-latest",
		Messages: []ClaudeMessage{{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"text","text":"hello"}]`),
		}},
		Tools: []ClaudeTool{
			{
				Name:        "get_weather",
				Description: "Get weather information",
				InputSchema: map[string]any{"type": "object"},
			},
			{
				Type: "web_search_20250305",
				Name: "web_search",
			},
		},
	}

	body, err := TransformClaudeToGeminiWithOptions(claudeReq, "project-1", "gemini-2.5-flash", DefaultTransformOptions())
	require.NoError(t, err)

	var req V1InternalRequest
	require.NoError(t, json.Unmarshal(body, &req))
	require.Len(t, req.Request.Tools, 2)
	require.Len(t, req.Request.Tools[0].FunctionDeclarations, 1)
	require.Equal(t, "get_weather", req.Request.Tools[0].FunctionDeclarations[0].Name)
	require.NotNil(t, req.Request.Tools[1].GoogleSearch)
}

func TestEnvelopeParity_ClaudeAndGeminiPathsMatch(t *testing.T) {
	request := GeminiRequest{
		Contents: []GeminiContent{{
			Role:  "user",
			Parts: []GeminiPart{{Text: "hello"}},
		}},
		SessionID: "session-1",
	}

	body, err := BuildV1InternalEnvelope("project-1", "gemini-2.5-flash", "agent", request)
	require.NoError(t, err)

	var got V1InternalRequest
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "project-1", got.Project)
	require.Equal(t, "antigravity", got.UserAgent)
	require.Equal(t, "agent", got.RequestType)
	require.Equal(t, "gemini-2.5-flash", got.Model)
	require.Equal(t, request.SessionID, got.Request.SessionID)
	require.Len(t, got.Request.Contents, 1)
	require.True(t, strings.HasPrefix(got.RequestID, "agent-"))
}

func TestGolden_ClaudeToV1Internal(t *testing.T) {
	tests := []struct {
		name       string
		request    *ClaudeRequest
		projectID  string
		mapped     string
		goldenFile string
	}{
		{
			name: "basic",
			request: &ClaudeRequest{
				Model:     "claude-sonnet-4-5",
				Messages:  []ClaudeMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
				MaxTokens: 16,
			},
			projectID:  "project-1",
			mapped:     "claude-sonnet-4-5",
			goldenFile: "claude_basic.golden.json",
		},
		{
			name: "with tools",
			request: &ClaudeRequest{
				Model:     "claude-sonnet-4-5",
				Messages:  []ClaudeMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
				MaxTokens: 16,
				Tools: []ClaudeTool{{
					Name:        "get_weather",
					Description: "Get weather",
					InputSchema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
				}},
			},
			projectID:  "project-1",
			mapped:     "claude-sonnet-4-5",
			goldenFile: "claude_with_tools.golden.json",
		},
		{
			name: "with thinking",
			request: &ClaudeRequest{
				Model:     "claude-opus-4-6-thinking",
				Messages:  []ClaudeMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
				MaxTokens: 2000,
				Thinking:  &ThinkingConfig{Type: "adaptive"},
			},
			projectID:  "project-1",
			mapped:     "claude-opus-4-6-thinking",
			goldenFile: "claude_with_thinking.golden.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := TransformClaudeToGeminiWithOptions(tt.request, tt.projectID, tt.mapped, DefaultTransformOptions())
			require.NoError(t, err)
			assertMatchesGolden(t, body, tt.goldenFile)
		})
	}
}

func assertMatchesGolden(t *testing.T, actual []byte, goldenFile string) {
	t.Helper()
	normalizedActual := normalizeGoldenJSON(t, actual)
	goldenPath := filepath.Join("testdata", goldenFile)
	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	normalizedExpected := normalizeGoldenJSON(t, expected)
	if *update {
		require.NoError(t, os.WriteFile(goldenPath, normalizedActual, 0o644))
		normalizedExpected = normalizedActual
	}
	require.JSONEq(t, string(normalizedExpected), string(normalizedActual))
}

var update = func() *bool {
	b := false
	return &b
}()

func normalizeGoldenJSON(t *testing.T, body []byte) []byte {
	t.Helper()
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	raw["requestId"] = "<request-id>"
	request, ok := raw["request"].(map[string]any)
	require.True(t, ok)
	if _, exists := request["sessionId"]; exists {
		request["sessionId"] = "<session-id>"
	}
	normalized, err := json.MarshalIndent(raw, "", "  ")
	require.NoError(t, err)
	return normalized
}
