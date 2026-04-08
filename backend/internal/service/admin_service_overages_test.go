//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateAccountOveragesRepoStub struct {
	mockAccountRepoForGemini
	account     *Account
	updateCalls int
}

func (r *updateAccountOveragesRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.account, nil
}

func (r *updateAccountOveragesRepoStub) Update(ctx context.Context, account *Account) error {
	r.updateCalls++
	r.account = account
	return nil
}

// [OpusClaw Patch] IsOveragesEnabled always returns true for Antigravity,
// so the "disable overages" transition never fires. This test verifies that
// removing allow_overages from Extra does NOT clear AICredits — overages stay on.
func TestUpdateAccount_OveragesAlwaysEnabledForAntigravity(t *testing.T) {
	accountID := int64(101)
	repo := &updateAccountOveragesRepoStub{
		account: &Account{
			ID:       accountID,
			Platform: PlatformAntigravity,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				"allow_overages":   true,
				"mixed_scheduling": true,
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					"claude-sonnet-4-5": {
						"rate_limited_at":     "2026-03-15T00:00:00Z",
						"rate_limit_reset_at": "2099-03-15T00:00:00Z",
					},
					creditsExhaustedKey: testRateLimitEntry(time.Now().Add(5 * time.Hour)),
				})[modelRateLimitsKey],
			},
		},
	}

	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Extra: map[string]any{
			"mixed_scheduling": true,
			modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
				"claude-sonnet-4-5": {
					"rate_limited_at":     "2026-03-15T00:00:00Z",
					"rate_limit_reset_at": "2099-03-15T00:00:00Z",
				},
				creditsExhaustedKey: testRateLimitEntry(time.Now().Add(5 * time.Hour)),
			})[modelRateLimitsKey],
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.True(t, updated.IsOveragesEnabled())

	rawLimits, ok := repo.account.Extra[modelRateLimitsKey].(map[string]any)
	require.True(t, ok)
	_, exists := rawLimits[creditsExhaustedKey]
	require.True(t, exists, "AICredits key preserved — overages always enabled")
	_, exists = rawLimits["claude-sonnet-4-5"]
	require.True(t, exists, "model rate limits preserved")
}

func TestUpdateAccount_EnableOveragesClearsModelRateLimitsBeforePersist(t *testing.T) {
	accountID := int64(102)
	repo := &updateAccountOveragesRepoStub{
		account: &Account{
			ID:       accountID,
			Platform: PlatformAntigravity,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				"mixed_scheduling": true,
				modelRateLimitsKey: testModelRateLimits(map[string]map[string]any{
					"claude-sonnet-4-5": {
						"rate_limited_at":     "2026-03-15T00:00:00Z",
						"rate_limit_reset_at": "2099-03-15T00:00:00Z",
					},
				})[modelRateLimitsKey],
			},
		},
	}

	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Extra: map[string]any{
			"mixed_scheduling": true,
			"allow_overages":   true,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.True(t, updated.IsOveragesEnabled())

	_, exists := repo.account.Extra[modelRateLimitsKey]
	require.False(t, exists, "开启 overages 时应在持久化前清掉旧模型限流")
}

func TestUpdateAccount_EmptyExtraPayloadCanClearQuotaLimits(t *testing.T) {
	accountID := int64(103)
	repo := &updateAccountOveragesRepoStub{
		account: &Account{
			ID:       accountID,
			Platform: PlatformAnthropic,
			Type:     AccountTypeAPIKey,
			Status:   StatusActive,
			Extra: map[string]any{
				"quota_limit":        100.0,
				"quota_daily_limit":  10.0,
				"quota_weekly_limit": 40.0,
			},
		},
	}

	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		// 显式空对象：语义是“清空 extra 中的可配置键”（例如关闭配额限制）
		Extra: map[string]any{},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 1, repo.updateCalls)
	require.NotNil(t, repo.account.Extra)
	require.NotContains(t, repo.account.Extra, "quota_limit")
	require.NotContains(t, repo.account.Extra, "quota_daily_limit")
	require.NotContains(t, repo.account.Extra, "quota_weekly_limit")
	require.Len(t, repo.account.Extra, 0)
}

// [OpusClaw Patch] IsOveragesEnabled always returns true for Antigravity, regardless of Extra.
func TestIsOveragesEnabled_AlwaysTrueForAntigravity(t *testing.T) {
	tests := []struct {
		name    string
		account Account
		want    bool
	}{
		{
			name:    "Antigravity without Extra",
			account: Account{Platform: PlatformAntigravity},
			want:    true,
		},
		{
			name:    "Antigravity with allow_overages=false",
			account: Account{Platform: PlatformAntigravity, Extra: map[string]any{"allow_overages": false}},
			want:    true,
		},
		{
			name:    "Antigravity with allow_overages=true",
			account: Account{Platform: PlatformAntigravity, Extra: map[string]any{"allow_overages": true}},
			want:    true,
		},
		{
			name:    "Antigravity with nil Extra",
			account: Account{Platform: PlatformAntigravity, Extra: nil},
			want:    true,
		},
		{
			name:    "Anthropic platform",
			account: Account{Platform: PlatformAnthropic},
			want:    false,
		},
		{
			name:    "OpenAI platform",
			account: Account{Platform: PlatformOpenAI},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.IsOveragesEnabled())
		})
	}
}
