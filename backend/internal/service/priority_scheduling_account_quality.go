package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// OAuth classification is a latest complete account decision, not an average
// of old model rounds. A single successful probe can refresh the confirmed
// classification but cannot replace it with an incomplete new decision.
func priorityOAuthQuality(item openAIAccountCandidateScore, signal PrioritySchedulingSignal, now time.Time, maxAge time.Duration) (bool, bool) {
	if item.priorityQualityCaptured {
		if item.priorityQualityPassed == nil || !now.Before(item.priorityQualityExpires) {
			return false, false
		}
		return *item.priorityQualityPassed, true
	}
	if quality, known := GatewayBorrowAccountQuality(item.account, now, maxAge); known {
		return quality.State == "healthy", true
	}
	if item.account.Extra[GatewayBorrowAccountQualityModeKey] == GatewayBorrowAccountQualityMode {
		return false, false
	}
	// Legacy deployments may still use internal completed quality rounds.
	// A later confirmed recovery must not be vetoed by an earlier poor average.
	if signal.LatestQualityPassed != nil {
		return *signal.LatestQualityPassed, true
	}
	return false, false
}

// Capture only sanitized decisions for snapshots. Required borrowing is judged
// on the usable verified route, while final admission still validates policy,
// owner, credentials and expiry immediately before sending.
func (s *OpenAIGatewayService) capturePriorityOAuthQuality(req OpenAIAccountScheduleRequest, item *openAIAccountCandidateScore, now time.Time, maxAge time.Duration) {
	a := item.account
	if a == nil || !a.IsOpenAIOAuth() {
		return
	}
	if a.RequiresGatewayBorrow(req.RequestedModel) {
		item.priorityQualityCaptured = true
		item.priorityQualityPassed = nil
		item.priorityQualityExpires = time.Time{}
		ctx := context.Background()
		if s.gatewayBorrowPolicyReason(ctx, a, req.RequestedModel, req.RequireCompact) != "" {
			return
		}
		expires := s.gatewayBorrowBindingExpiry(ctx, a, a.GetMappedModel(req.RequestedModel))
		if !now.Before(expires) {
			return
		}
		passed := true
		item.priorityQualityPassed, item.priorityQualityExpires = &passed, expires
		return
	}
	if a.Extra[GatewayBorrowAccountQualityModeKey] != GatewayBorrowAccountQualityMode {
		return
	}
	item.priorityQualityCaptured = true
	item.priorityQualityPassed = nil
	item.priorityQualityExpires = time.Time{}
	if !GatewayBorrowAccountQualityLinkedModel(config.CanonicalGatewayBorrowModel(normalizeExcelBPSIsolationModel(a.GetMappedModel(req.RequestedModel)))) {
		return
	}
	if quality, known := GatewayBorrowAccountQuality(a, now, maxAge); known {
		passed := quality.State == "healthy"
		item.priorityQualityPassed = &passed
		item.priorityQualityExpires = quality.LatestProbeAt.Add(maxAge)
	}
}
