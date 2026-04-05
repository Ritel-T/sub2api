//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateSimCacheKey_NilParsedRequest(t *testing.T) {
	svc := &GatewayService{}
	require.Empty(t, svc.GenerateSimCacheKey(nil))
}

func TestGenerateSimCacheKey_MetadataHasHighestPriority(t *testing.T) {
	svc := &GatewayService{}

	parsed := &ParsedRequest{
		MetadataUserID: `{"device_id":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","account_uuid":"","session_id":"c72554f2-1234-5678-abcd-123456789abc"}`,
		System:         "You are a helpful assistant.",
		HasSystem:      true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}

	key := svc.GenerateSimCacheKey(parsed)
	require.Equal(t, "c72554f2-1234-5678-abcd-123456789abc", key)
}

func TestGenerateSimCacheKey_StableAcrossIPChanges(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}
	parsed2 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "5.6.7.8", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}

	key1 := svc.GenerateSimCacheKey(parsed1)
	key2 := svc.GenerateSimCacheKey(parsed2)
	require.NotEmpty(t, key1)
	require.Equal(t, key1, key2)
}

func TestGenerateSimCacheKey_StableAcrossUAChanges(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}
	parsed2 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "Go-http-client/1.1", APIKeyID: 42},
	}

	key1 := svc.GenerateSimCacheKey(parsed1)
	key2 := svc.GenerateSimCacheKey(parsed2)
	require.NotEmpty(t, key1)
	require.Equal(t, key1, key2)
}

func TestGenerateSimCacheKey_DifferentAPIKeyIDProducesDifferentKey(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}
	parsed2 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "claude-cli/2.1.78", APIKeyID: 99},
	}

	key1 := svc.GenerateSimCacheKey(parsed1)
	key2 := svc.GenerateSimCacheKey(parsed2)
	require.NotEmpty(t, key1)
	require.NotEmpty(t, key2)
	require.NotEqual(t, key1, key2)
}

func TestGenerateSimCacheKey_DynamicSystemExcludedFromKey(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "You are OpenCode. Today's date: 4/5/2026. Files: a b c.",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{APIKeyID: 42},
	}
	parsed2 := &ParsedRequest{
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "You are OpenCode. Today's date: 4/5/2026. Files: a b c d. Todo: updated.",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{APIKeyID: 42},
	}

	key1 := svc.GenerateSimCacheKey(parsed1)
	key2 := svc.GenerateSimCacheKey(parsed2)
	require.NotEmpty(t, key1)
	require.Equal(t, key1, key2)
}

func TestGenerateSimCacheKey_StringSystemIncludedInKey(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{APIKeyID: 42},
	}
	parsed2 := &ParsedRequest{
		System:    "You are a debugging assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{APIKeyID: 42},
	}

	key1 := svc.GenerateSimCacheKey(parsed1)
	key2 := svc.GenerateSimCacheKey(parsed2)
	require.NotEmpty(t, key1)
	require.NotEmpty(t, key2)
	require.NotEqual(t, key1, key2)
}

func TestGenerateSimCacheKey_EphemeralSystemWithDifferentAPIKeys(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "You are a tool-specific assistant.",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.1.1.1", UserAgent: "ua1", APIKeyID: 100},
	}
	parsed2 := &ParsedRequest{
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "You are a tool-specific assistant.",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "2.2.2.2", UserAgent: "ua2", APIKeyID: 200},
	}

	key1 := svc.GenerateSimCacheKey(parsed1)
	key2 := svc.GenerateSimCacheKey(parsed2)
	require.NotEqual(t, key1, key2)
}

func TestGenerateSessionHash_RemainsSensitiveToIPChanges(t *testing.T) {
	svc := &GatewayService{}

	parsed1 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "1.2.3.4", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}
	parsed2 := &ParsedRequest{
		System:    "You are a helpful assistant.",
		HasSystem: true,
		Messages: []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		SessionContext: &SessionContext{ClientIP: "5.6.7.8", UserAgent: "claude-cli/2.1.78", APIKeyID: 42},
	}

	hash1 := svc.GenerateSessionHash(parsed1)
	hash2 := svc.GenerateSessionHash(parsed2)
	require.NotEqual(t, hash1, hash2)
}
