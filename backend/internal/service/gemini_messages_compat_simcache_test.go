package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

func TestExtractGeminiUsageWithOverride_FirstTurn(t *testing.T) {
	data := []byte(`{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":0,"candidatesTokenCount":12,"thoughtsTokenCount":3}}`)
	usage := extractGeminiUsage(data, &antigravity.SimCacheOverride{IsFirstTurn: true})
	require.NotNil(t, usage)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 0, usage.CacheReadInputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 15, usage.OutputTokens)
}

func TestExtractGeminiUsageWithOverride_Hit(t *testing.T) {
	data := []byte(`{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":0,"candidatesTokenCount":12,"thoughtsTokenCount":3}}`)
	usage := extractGeminiUsage(data, &antigravity.SimCacheOverride{HistoryCachedTokenCount: 80})
	require.NotNil(t, usage)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 80, usage.CacheReadInputTokens)
	require.Equal(t, 20, usage.CacheCreationInputTokens)
	require.Equal(t, 15, usage.OutputTokens)
}

func TestExtractGeminiUsageWithOverride_Miss(t *testing.T) {
	data := []byte(`{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":0,"candidatesTokenCount":12,"thoughtsTokenCount":3}}`)
	usage := extractGeminiUsage(data, &antigravity.SimCacheOverride{HistoryCachedTokenCount: 80, IsMiss: true})
	require.NotNil(t, usage)
	require.Equal(t, 0, usage.InputTokens)
	require.Equal(t, 0, usage.CacheReadInputTokens)
	require.Equal(t, 100, usage.CacheCreationInputTokens)
	require.Equal(t, 15, usage.OutputTokens)
}
