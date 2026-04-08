package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCrossModelIsolation(t *testing.T) {
	// 常量定义，确保使用正确的 model keys
	// 根据 domain.DefaultAntigravityModelMapping:
	// "claude-sonnet-4-5" -> "claude-sonnet-4-5"
	// "gemini-2.5-flash" -> "gemini-2.5-flash"
	const (
		claudeModel = "claude-sonnet-4-5"
		geminiModel = "gemini-2.5-flash"
		creditsKey  = "AICredits"
	)

	future := time.Now().Add(10 * time.Minute).Format(time.RFC3339)

	t.Run("TestCrossModelIsolation_ClaudeRateLimited_GeminiSchedulable", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					claudeModel: {"rate_limit_reset_at": future},
					creditsKey:  {"rate_limit_reset_at": future},
				})[modelRateLimitsKey],
				"allow_overages": true,
			},
		}

		ctx := context.Background()
		// Claude 应该被限流（因为模型被限流且积分已耗尽）
		require.False(t, account.IsSchedulableForModelWithContext(ctx, claudeModel))
		// Gemini 应该可调度（因为模型未被限流，不受积分耗尽影响）
		require.True(t, account.IsSchedulableForModelWithContext(ctx, geminiModel))
	})

	t.Run("TestCrossModelIsolation_GeminiRateLimited_ClaudeSchedulable", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					geminiModel: {"rate_limit_reset_at": future},
					creditsKey:  {"rate_limit_reset_at": future},
				})[modelRateLimitsKey],
				"allow_overages": true,
			},
		}

		ctx := context.Background()
		// Gemini 应该被限流
		require.False(t, account.IsSchedulableForModelWithContext(ctx, geminiModel))
		// Claude 应该可调度
		require.True(t, account.IsSchedulableForModelWithContext(ctx, claudeModel))
	})

	t.Run("TestCrossModelIsolation_BothRateLimited_CreditsExhausted", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					claudeModel: {"rate_limit_reset_at": future},
					geminiModel: {"rate_limit_reset_at": future},
					creditsKey:  {"rate_limit_reset_at": future},
				})[modelRateLimitsKey],
				"allow_overages": true,
			},
		}

		ctx := context.Background()
		// 两个模型都应该被限流
		require.False(t, account.IsSchedulableForModelWithContext(ctx, claudeModel))
		require.False(t, account.IsSchedulableForModelWithContext(ctx, geminiModel))
	})

	t.Run("TestCrossModelIsolation_ModelRateLimited_CreditsAvailable", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					claudeModel: {"rate_limit_reset_at": future},
				})[modelRateLimitsKey],
				"allow_overages": true,
			},
		}

		ctx := context.Background()
		// Claude 虽然被限流，但积分可用且 allow_overages=true (IsOveragesEnabled() 对于 Antigravity 始终返回 true)，所以应该可调度
		require.True(t, account.IsSchedulableForModelWithContext(ctx, claudeModel))
	})

	t.Run("TestCrossModelIsolation_AICreditsShared_BothBenefit", func(t *testing.T) {
		account := &Account{
			Platform:    PlatformAntigravity,
			Status:      StatusActive,
			Schedulable: true,
			Extra: map[string]any{
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					claudeModel: {"rate_limit_reset_at": future},
					geminiModel: {"rate_limit_reset_at": future},
				})[modelRateLimitsKey],
				"allow_overages": true,
			},
		}

		ctx := context.Background()
		// 两个模型虽然都被限流，但共用积分可用，所以都应该可调度
		require.True(t, account.IsSchedulableForModelWithContext(ctx, claudeModel))
		require.True(t, account.IsSchedulableForModelWithContext(ctx, geminiModel))
	})
}
