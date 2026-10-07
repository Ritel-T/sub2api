package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPrioritySoftStickyKeepsExactExistingSelectionPlan(t *testing.T) {
	for _, previous := range []bool{false, true} {
		t.Run(map[bool]string{false: "session", true: "movable_previous"}[previous], func(t *testing.T) {
			priority := DefaultPrioritySchedulingConfig()
			priority.Enabled = true
			gateway := priorityGateway(priority, &priorityReaderStub{})
			gateway.cfg = &config.Config{}
			gateway.cfg.Gateway.OpenAIWS.LBTopK = 2
			gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 1
			gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1
			gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.SessionSticky = 3
			gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.PreviousResponse = 3
			scheduler := &defaultOpenAIAccountScheduler{service: gateway}
			sticky := priorityCandidate(1, .1, 20).account
			peer := priorityCandidate(2, .1, 0).account
			sticky.Priority, peer.Priority = 100, 0
			req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-6.1-sol", UseUpstreamTokenCost: true, StickyWeighted: true, SessionHash: "existing-conversation", StickyAccountID: 1}
			if previous {
				req.StickyAccountID, req.StickyPreviousAccountID, req.PreviousResponseCanMove = 0, 1, true
				req.PreviousResponseID = "movable-response"
			}
			loads := map[int64]*AccountLoadInfo{1: {AccountID: 1, LoadRate: 20, CurrentConcurrency: 2}, 2: {AccountID: 2}}
			enabled := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, []*Account{sticky, peer}, loads)
			require.False(t, enabled.priorityScheduling, "new policy must leave existing weighted route intact")
			priority.Enabled = false
			require.NoError(t, gateway.settingService.SavePrioritySchedulingConfig(context.Background(), priority))
			disabled := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, []*Account{sticky, peer}, loads)
			require.Equal(t, disabled, enabled, "scores, TopK, original sticky preference and order remain identical")
			require.Equal(t, int64(1), enabled.selectionOrder[0].account.ID)
		})
	}
}

func TestPrioritySoftStickyCannotReintroduceFilteredAccount(t *testing.T) {
	cfg := DefaultPrioritySchedulingConfig()
	cfg.Enabled = true
	gateway := priorityGateway(cfg, &priorityReaderStub{})
	scheduler := &defaultOpenAIAccountScheduler{service: gateway}
	for _, req := range []OpenAIAccountScheduleRequest{
		{StickyWeighted: true, StickyAccountID: 99},
		{StickyWeighted: true, StickyPreviousAccountID: 99, PreviousResponseCanMove: true},
		{StickyWeighted: true, StickyPreviousAccountID: 1, PreviousResponseCanMove: false},
		{StickyWeighted: true},
	} {
		req.Platform, req.RequestedModel, req.UseUpstreamTokenCost = PlatformOpenAI, "gpt-6.1-sol", true
		plan := openAIAccountLoadPlan{candidates: []openAIAccountCandidateScore{priorityCandidate(1, .1, 0)}}
		require.True(t, scheduler.applyPriorityScheduling(req, &plan))
		require.True(t, plan.priorityScheduling)
		require.Len(t, plan.selectionOrder, 1)
		require.Equal(t, int64(1), plan.selectionOrder[0].account.ID)
	}
}

func TestOAuthCostNeverChangesExistingWeightedSoftStickyScores(t *testing.T) {
	priority := DefaultPrioritySchedulingConfig()
	priority.Enabled = true
	gateway := priorityGateway(priority, &priorityReaderStub{})
	gateway.cfg = &config.Config{}
	gateway.cfg.Gateway.OpenAIWS.LBTopK = 2
	gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Load = 1
	gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.SessionSticky = 3
	gateway.cfg.Gateway.OpenAIWS.SchedulerScoreWeights.UpstreamCost = 100
	scheduler := &defaultOpenAIAccountScheduler{service: gateway}
	a, b := priorityCandidate(1, .1, 0).account, priorityCandidate(2, .8, 0).account
	a.Type, b.Type = AccountTypeOAuth, AccountTypeOAuth
	req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-6.1-sol", UseUpstreamTokenCost: true, StickyWeighted: true, SessionHash: "existing-conversation", StickyAccountID: 1}
	loads := map[int64]*AccountLoadInfo{1: {AccountID: 1}, 2: {AccountID: 2}}
	before := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, []*Account{a, b}, loads)
	for _, cost := range []float64{0, 1e6, math.NaN()} {
		a.Extra[AccountCostMultiplierExtraKey] = cost
		a.RateMultiplier = floatPtr(cost)
		b.RateMultiplier = floatPtr(1e6)
		after := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, []*Account{a, b}, loads)
		require.Equal(t, before.candidates[0].score, after.candidates[0].score)
		require.Equal(t, before.candidates[1].score, after.candidates[1].score)
		require.Equal(t, int64(1), after.selectionOrder[0].account.ID)
	}
}
