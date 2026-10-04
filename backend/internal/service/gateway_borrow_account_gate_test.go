package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGatewayBorrowAutomaticWarmSkipsKnownNativeCooldown(t *testing.T) {
	now := time.Now()
	later := now.Add(5 * 24 * time.Hour)
	for _, field := range []string{"global", "temporary", "overload", "model", "error", "paused"} {
		t.Run(field, func(t *testing.T) {
			account := &Account{ID: 111, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{}}
			switch field {
			case "global":
				account.RateLimitResetAt = &later
			case "temporary":
				account.TempUnschedulableUntil = &later
			case "overload":
				account.OverloadUntil = &later
			case "model":
				account.Extra[modelRateLimitsKey] = map[string]any{"gpt-6-astra": map[string]any{"rate_limit_reset_at": later.Format(time.RFC3339)}}
			case "error":
				account.Status = StatusError
			case "paused":
				account.Schedulable = false
			}
			settings := config.AstraRoutingSettings{AutoQuality: true, CookiePool: config.CodexGatewayPinConfig{Enabled: true, TargetAccountIDs: []int64{111}, Models: []string{"gpt-6-astra", "gpt-6.1-sol"}}}
			cfg := &config.Config{}
			cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
			svc := &AccountTestService{cfg: cfg, accountRepo: &stateProbeAccountRepo{account: account}}
			err := svc.verifyGatewayBorrowTargetForModel(t.Context(), 111, "gpt-6-astra")
			if field == "error" || field == "paused" {
				require.ErrorContains(t, err, "target_account_unavailable")
			} else {
				require.ErrorContains(t, err, "target_account_rate_limited")
			}
			runtime := svc.AstraGatewayStatus(t.Context())
			require.Equal(t, "blocked", runtime.Targets[0].State)
			if field != "error" && field != "paused" {
				require.NotNil(t, runtime.Targets[0].RetryAt)
			}
			if field == "model" {
				reason, _ := gatewayBorrowAccountBlock(account, "gpt-6.1-sol", now)
				require.Empty(t, reason, "Astra-specific restriction must leave Sol native warm eligible")
			}
		})
	}
}

func TestGatewayBorrowAccountGateIgnoresBPSCooldown(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Hour)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{ExcelBPSRateLimitResetAtKey: until.Format(time.RFC3339)}}
	reason, retry := gatewayBorrowAccountBlock(account, "gpt-6-astra", now)
	require.Empty(t, reason)
	require.Nil(t, retry)
	account.RateLimitResetAt = &now
	reason, retry = gatewayBorrowAccountBlock(account, "gpt-6-astra", now.Add(time.Second))
	require.Empty(t, reason)
	require.Nil(t, retry, "expired native deadlines cannot suppress verification")
}
