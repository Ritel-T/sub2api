package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	// [OpusClaw Patch] creditsExhaustedKey is the legacy model-rate-limit key used to
	// represent the local credits-path cooldown policy for Antigravity accounts.
	creditsExhaustedKey = "AICredits"
	// [OpusClaw Patch] creditsExhaustedDuration is a local scheduling cooldown, not
	// a verified upstream truth about actual AI Credits balance.
	creditsExhaustedDuration = 30 * time.Minute
)

// [OpusClaw Patch] IsCreditsPathPaused reports whether the local Antigravity
// credits-path cooldown policy is currently active.
func (a *Account) IsCreditsPathPaused() bool {
	if a == nil {
		return false
	}
	return a.isRateLimitActiveForKey(creditsExhaustedKey)
}

// [OpusClaw Patch] GetCreditsPolicyResetAt returns the reset time for the local
// credits-path cooldown policy, if present.
func (a *Account) GetCreditsPolicyResetAt() *time.Time {
	if a == nil {
		return nil
	}
	return a.modelRateLimitResetAt(creditsExhaustedKey)
}

// [OpusClaw Patch] GetCreditsPolicyRemaining returns remaining duration for the
// local credits-path cooldown policy. Zero means inactive or expired.
func (a *Account) GetCreditsPolicyRemaining() time.Duration {
	if a == nil {
		return 0
	}
	return a.getRateLimitRemainingForKey(creditsExhaustedKey)
}

// [OpusClaw Patch] setCreditsPathPaused marks the local credits-path cooldown
// policy active by writing the legacy AICredits key into model_rate_limits.
func (s *AntigravityGatewayService) setCreditsPathPaused(ctx context.Context, account *Account) {
	if account == nil || account.ID == 0 {
		return
	}
	resetAt := time.Now().Add(creditsExhaustedDuration)
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, creditsExhaustedKey, resetAt); err != nil {
		logger.LegacyPrintf("service.antigravity_gateway", "set credits exhausted failed: account=%d err=%v", account.ID, err)
		return
	}
	s.updateAccountModelRateLimitInCache(ctx, account, creditsExhaustedKey, resetAt)
	logger.LegacyPrintf("service.antigravity_gateway", "credits_exhausted_marked account=%d reset_at=%s",
		account.ID, resetAt.UTC().Format(time.RFC3339))
}

// [OpusClaw Patch] clearCreditsPathPaused clears the local credits-path cooldown
// policy by removing the legacy AICredits key from model_rate_limits.
func (s *AntigravityGatewayService) clearCreditsPathPaused(ctx context.Context, account *Account) {
	if account == nil || account.ID == 0 || account.Extra == nil {
		return
	}
	rawLimits, ok := account.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return
	}
	if _, exists := rawLimits[creditsExhaustedKey]; !exists {
		return
	}
	delete(rawLimits, creditsExhaustedKey)
	account.Extra[modelRateLimitsKey] = rawLimits
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		modelRateLimitsKey: rawLimits,
	}); err != nil {
		logger.LegacyPrintf("service.antigravity_gateway", "clear credits exhausted failed: account=%d err=%v", account.ID, err)
	}
	if s.schedulerSnapshot != nil {
		if err := s.schedulerSnapshot.UpdateAccountInCache(ctx, account); err != nil {
			logger.LegacyPrintf("service.antigravity_gateway", "clear credits exhausted cache update failed: account=%d err=%v", account.ID, err)
		}
	}
}
