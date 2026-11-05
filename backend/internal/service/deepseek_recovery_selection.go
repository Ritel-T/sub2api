package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
)

type deepSeekRecoverySelectionContextKey struct{}

// WithDeepSeekRecoverySelection prevents recovery attempts from replacing the
// original sticky binding. The DeepSeek recovery coordinator must pass this
// context through selection AND post-slot admission; keep the session hash for
// upstream identity and billing. First attempts use the ordinary context.
func WithDeepSeekRecoverySelection(ctx context.Context) context.Context {
	return context.WithValue(ctx, deepSeekRecoverySelectionContextKey{}, true)
}

func deepSeekRecoverySelectionFromContext(ctx context.Context) bool {
	recovery, _ := ctx.Value(deepSeekRecoverySelectionContextKey{}).(bool)
	return recovery
}

// SelectDeepSeekRecoveryAccount selects only same-group DeepSeek accounts for a
// Chat Completions-capable recovery attempt. It keeps the ordinary eligibility
// and slot admission gates, but never consults affinity, priority or TopK.
// The caller owns exclude and adds each dispatched account before retrying.
func (s *OpenAIGatewayService) SelectDeepSeekRecoveryAccount(ctx context.Context, groupID *int64, model string, exclude map[int64]struct{}) (*AccountSelectionResult, error) {
	defer requesttiming.Observe(ctx, "account_selection")()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = WithDeepSeekRecoverySelection(ctx)
	ctx = s.withExcelBPSCooldownContext(ctx)
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	ctx = s.withOpenAIGroupPrivacyRequirement(ctx, groupID)
	ctx = s.withOpenAIProfitControlGate(ctx, groupID)
	if s.checkChannelPricingRestriction(ctx, groupID, model) {
		return nil, fmt.Errorf("%w supporting model: %s (channel pricing restriction)", ErrNoAvailableAccounts, model)
	}

	req := OpenAIAccountScheduleRequest{
		GroupID:               groupID,
		Platform:              domain.PlatformDeepseek,
		RequestedModel:        model,
		ExcludedIDs:           exclude,
		RequiredTransport:     OpenAIUpstreamTransportHTTPSSE,
		RequiredCapability:    OpenAIEndpointCapabilityChatCompletions,
		RequirePrivacySet:     s.openAIGroupRequiresPrivacySet(ctx, groupID),
		PreserveStickyBinding: true,
	}
	// Reuse the shared hard gates, without entering Select's affinity or ranking
	// layers. Cached load is not authoritative: probe every eligible slot before
	// returning a wait plan, even if a cached load reading says it is full.
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	ctx, candidates, _, _, err := scheduler.loadOpenAIEligibleAccounts(ctx, req)
	if err != nil {
		return nil, err
	}
	order := make([]openAIAccountCandidateScore, 0, len(candidates))
	seen := make(map[int64]struct{}, len(candidates))
	for _, account := range candidates {
		if !openAIStickyAccountMatchesGroup(account, groupID) {
			continue
		}
		if _, duplicate := seen[account.ID]; duplicate {
			continue
		}
		seen[account.ID] = struct{}{}
		order = append(order, openAIAccountCandidateScore{account: account})
	}
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	budget := newOpenAISelectionProbeBudget() // no TopK/probe-limit truncation
	for i := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selection, _, err := scheduler.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, order[i:i+1], budget)
		if err != nil {
			return nil, err
		}
		if selection == nil {
			continue
		}
		// Enforce same-group isolation after the shared fresh/DB recheck too;
		// ordinary simple-mode selection deliberately relaxes that restriction.
		if ctx.Err() != nil || !openAIStickyAccountMatchesGroup(selection.Account, groupID) {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			continue
		}
		return selection, nil
	}

	// No immediately available account: reuse the configured bounded fallback
	// wait. The handler owns queue admission, waiting and the final slot check.
	cfg := s.schedulingConfig()
	for _, candidate := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fresh := s.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, groupID, domain.PlatformDeepseek, model, false, req.RequiredCapability)
		fresh = s.recheckSelectedOpenAIAccountFromDB(ctx, fresh, groupID, domain.PlatformDeepseek, model, false, req.RequiredCapability)
		if !openAIStickyAccountMatchesGroup(fresh, groupID) || !scheduler.isAccountRequestCompatible(ctx, fresh, req) {
			continue
		}
		return attachSelectionProfitGate(ctx, &AccountSelectionResult{
			Account: fresh,
			WaitPlan: &AccountWaitPlan{
				AccountID:      fresh.ID,
				MaxConcurrency: fresh.Concurrency,
				Timeout:        cfg.FallbackWaitTimeout,
				MaxWaiting:     cfg.FallbackMaxWaiting,
			},
		}), nil
	}
	return nil, ErrNoAvailableAccounts
}

// RevalidateDeepSeekRecoveryAccount checks the account after slot acquisition,
// before starting an upstream attempt. The caller owns the acquired slot and
// must release it on error. This does not select, acquire, or bind an account.
func (s *OpenAIGatewayService) RevalidateDeepSeekRecoveryAccount(ctx context.Context, groupID *int64, account *Account, model string) (*Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || account == nil || account.ID <= 0 || account.Platform != domain.PlatformDeepseek {
		return nil, ErrNoAvailableAccounts
	}
	// Ordinary scheduler cache entries can predate a wait. Read the primary
	// repository directly; regional readers retain their own authoritative TTL
	// and persistence fence instead of falling back to the central repository.
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var latest *Account
	var err error
	if s.schedulerSnapshot != nil && s.schedulerSnapshot.localSnapshotReader != nil {
		latest, err = s.schedulerSnapshot.GetAccount(readCtx, account.ID)
	} else if s.accountRepo != nil {
		latest, err = s.accountRepo.GetByID(readCtx, account.ID)
	} else {
		return nil, ErrNoAvailableAccounts
	}
	if err != nil {
		return nil, fmt.Errorf("%w: DeepSeek recovery account refresh: %w", ErrNoAvailableAccounts, err)
	}
	if err := readCtx.Err(); err != nil {
		return nil, err
	}
	if latest == nil || latest.ID != account.ID || !openAIStickyAccountMatchesGroup(latest, groupID) {
		return nil, ErrNoAvailableAccounts
	}
	// The slot was acquired against the old limit. A newly reduced limit must
	// go back through selection/acquisition rather than reuse that admission.
	if latest.Concurrency > 0 && (account.Concurrency <= 0 || latest.Concurrency < account.Concurrency) {
		return nil, ErrNoAvailableAccounts
	}
	ctx = s.withExcelBPSCooldownContext(ctx)
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	ctx = s.withOpenAIGroupPrivacyRequirement(ctx, groupID)
	ctx = s.withOpenAIProfitControlGate(ctx, groupID)
	if s.checkChannelPricingRestriction(ctx, groupID, model) ||
		!isOpenAICompatibleAccountEligibleForRequest(ctx, latest, groupID, domain.PlatformDeepseek, model, false, OpenAIEndpointCapabilityChatCompletions) ||
		s.isOpenAIAccountBlockedBySchedulingThreshold(ctx, latest) {
		return nil, ErrNoAvailableAccounts
	}
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	if !scheduler.isAccountRequestCompatible(ctx, latest, OpenAIAccountScheduleRequest{
		GroupID: groupID, Platform: domain.PlatformDeepseek, RequestedModel: model,
		RequiredTransport: OpenAIUpstreamTransportHTTPSSE, RequiredCapability: OpenAIEndpointCapabilityChatCompletions,
		RequirePrivacySet: s.openAIGroupRequiresPrivacySet(ctx, groupID), PreserveStickyBinding: true,
	}) {
		return nil, ErrNoAvailableAccounts
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return latest, nil
}
