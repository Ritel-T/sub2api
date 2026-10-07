package service

import (
	"context"
	"math"
	"sort"
	"time"
)

const priorityExplorationShare = 0.10

// Ten-percentage-point cohorts prefer genuinely idle capacity while preserving
// weighted diversity among similarly loaded accounts. Use the next slot and
// RPM headroom, and rank real queues after every non-queued cohort.
func priorityCapacityBand(item openAIAccountCandidateScore) int {
	if !item.loadKnown || item.loadInfo == nil || item.account.Concurrency <= 0 {
		return 10
	}
	load := float64(max(0, item.loadInfo.CurrentConcurrency)+1) / float64(item.account.Concurrency)
	if item.rpmEnabled && item.rpmLimit > 0 {
		load = math.Max(load, float64(item.rpmCurrent+1)/float64(item.rpmLimit))
	}
	band := int(math.Floor(load * 10))
	if item.loadInfo.WaitingCount > 0 {
		return 20 + max(band, item.loadInfo.WaitingCount)
	}
	return band
}

func (s *OpenAIGatewayService) balancesPriorityProtocols(req OpenAIAccountScheduleRequest) bool {
	if s == nil || req.Platform != PlatformOpenAI || !req.UseUpstreamTokenCost || req.RequiredImageCapability != "" {
		return false
	}
	cfg := s.prioritySchedulingRuntimeConfig()
	return cfg.BalanceProtocols && cfg.applies(req.GroupID, req.RequestedModel)
}

// Count distinct bindings, not group price/name or an inferred account class.
// AccountGroups supports older snapshots that do not populate GroupIDs.
func priorityAccountGroupCount(account *Account) int {
	if account == nil {
		return 1
	}
	groups := make(map[int64]struct{}, len(account.GroupIDs)+len(account.AccountGroups))
	for _, id := range account.GroupIDs {
		if id > 0 {
			groups[id] = struct{}{}
		}
	}
	for _, binding := range account.AccountGroups {
		if binding.GroupID > 0 {
			groups[binding.GroupID] = struct{}{}
		}
	}
	return max(1, len(groups))
}

// prioritySelectionWeight bounds the score preference so every usable account
// in a tier/priority cohort can receive traffic. Capacity uses real slots, not
// load_factor, and quota headroom is a soft preference rather than a new gate.
func prioritySelectionWeight(item openAIAccountCandidateScore, now time.Time) float64 {
	return prioritySelectionWeightWithQuota(item, openAIQuotaHeadroomFactor(item.account, now))
}

func prioritySelectionWeightWithQuota(item openAIAccountCandidateScore, headroom float64) float64 {
	remaining := 1.0
	if item.loadKnown && item.loadInfo != nil {
		remaining = math.Max(0.05, float64(max(1, item.account.Concurrency)-max(0, item.loadInfo.CurrentConcurrency))) /
			(1 + float64(max(0, item.loadInfo.WaitingCount)))
	}
	score := math.Mod(item.score, 200)
	if math.IsNaN(score) || math.IsInf(score, 0) {
		score = 0
	}
	preference := 0.5 + clamp01(score/100)
	if math.IsNaN(headroom) || math.IsInf(headroom, 0) {
		headroom = openAIQuotaHeadroomNeutralFactor
	}
	quota := 0.5 + 0.5*clamp01(headroom)
	// OAuth capacity is shared by every bound group. Prefer spare capacity
	// exposed to fewer groups without excluding broader accounts or reserving
	// idle slots when there is work they can serve. API-key routing weights keep
	// their existing model because upstream capacity may not be subscription-bound.
	groups := 1
	if item.account.IsOpenAIOAuth() {
		groups = priorityAccountGroupCount(item.account)
	}
	latency := item.priorityLatencyFactor
	if latency <= 0 || math.IsNaN(latency) || math.IsInf(latency, 0) {
		latency = 1
	}
	return remaining * preference * quota * latency / float64(groups)
}

// Priority scheduling only changes free selection. Keep the existing sticky
// error/latency/full-slot behavior; enabling this feature must not migrate a
// healthy bound conversation because another account has more spare capacity.
func (s *defaultOpenAIAccountScheduler) shouldRebalancePrioritySticky(_ context.Context, _ OpenAIAccountScheduleRequest, _ *Account) bool {
	return false
}

// Weighted sampling without replacement avoids a deterministic best-account
// monopoly and keeps all overflow candidates. Exponential races are O(n log n)
// and require no cross-instance counters or per-session scheduler state.
func buildPrioritySelectionOrder(pool []openAIAccountCandidateScore, req OpenAIAccountScheduleRequest) []openAIAccountCandidateScore {
	type choice struct {
		candidate openAIAccountCandidateScore
		key       float64
		band      int
	}
	now := time.Now()
	rng := newOpenAISelectionRNG(deriveOpenAISelectionSeed(req))
	choices := make([]choice, len(pool))
	for i, candidate := range pool {
		choices[i] = choice{candidate: candidate, key: -math.Log1p(-rng.nextFloat64()) / prioritySelectionWeight(candidate, now), band: priorityCapacityBand(candidate)}
	}
	sort.Slice(choices, func(i, j int) bool {
		a, b := choices[i].candidate, choices[j].candidate
		if a.priorityUnhealthy != b.priorityUnhealthy {
			return !a.priorityUnhealthy
		}
		if choices[i].band != choices[j].band {
			return choices[i].band < choices[j].band
		}
		if int(a.score/200) != int(b.score/200) {
			return a.score > b.score
		}
		if a.account.Priority != b.account.Priority {
			return a.account.Priority < b.account.Priority
		}
		if choices[i].key != choices[j].key {
			return choices[i].key < choices[j].key
		}
		return a.account.ID < b.account.ID
	})

	// Cold accounts otherwise never gain enough evidence when an
	// established eligible cohort exists. Reserve a 10% exploration probability
	// for safe unknown accounts at that same explicit priority. Known degraded
	// accounts, busy accounts, protocol partitions and hard bindings cannot use
	// this lane; the ordinary eligibility/profit/slot checks still follow.
	if len(choices) > 1 && int(choices[0].candidate.score/200) == 2 && rng.nextFloat64() < priorityExplorationShare {
		best := -1
		for i := 1; i < len(choices); i++ {
			candidate := choices[i].candidate
			if int(candidate.score/200) == 1 && candidate.priorityExploration &&
				!candidate.priorityUnhealthy && choices[i].band <= choices[0].band &&
				candidate.account.Priority == choices[0].candidate.account.Priority &&
				(best < 0 || choices[i].key < choices[best].key) {
				best = i
			}
		}
		if best > 0 {
			selected := choices[best]
			copy(choices[1:best+1], choices[:best])
			choices[0] = selected
		}
	}
	order := make([]openAIAccountCandidateScore, len(choices))
	for i := range choices {
		order[i] = choices[i].candidate
	}
	return applyPriorityOAuthStandby(order)
}
