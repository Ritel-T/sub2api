package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPriorityOAuthLatestAccountConfirmationControlsSolAndAstra(t *testing.T) {
	now := time.Now().UTC()
	cfg := DefaultPrioritySchedulingConfig()
	cfg.Enabled = true
	a, _ := accountQualityFixture(now, "healthy")
	a.ID, a.Concurrency = 701, 10
	// Historical independent Sol and old aggregate failures cannot veto the
	// account's newest completed Astra confirmation (three correct of four).
	bad := false
	signal := PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500, QualityPassed: 1, QualitySamples: 20, LatestQualityPassed: &bad, ProfitSamples: 20, Revenue: 0, BaseCost: 10000}
	svc := priorityGateway(cfg, &priorityReaderStub{signal: map[int64]PrioritySchedulingSignal{a.ID: signal}})
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol"} {
		candidate := openAIAccountCandidateScore{account: a, loadKnown: true, loadInfo: &AccountLoadInfo{AccountID: a.ID}}
		svc.capturePriorityOAuthQuality(OpenAIAccountScheduleRequest{RequestedModel: model}, &candidate, now, 24*time.Hour)
		score := applyPriorityCandidate(cfg, &candidate, signal, now)
		require.Equal(t, "eligible", score.Tier, model)
		require.NotNil(t, score.LatestQualityPassed)
		require.True(t, *score.LatestQualityPassed)
		require.NotContains(t, score.Reasons, "quality_below_target")
		require.NotContains(t, score.Reasons, "historical_loss")
		require.False(t, candidate.priorityUnhealthy)
	}
}

func TestPriorityOAuthIncompleteNewClassificationCannotUseLegacyPass(t *testing.T) {
	now := time.Now().UTC()
	for _, mutate := range []func(*Account, map[string]any){
		func(a *Account, _ map[string]any) { delete(a.Extra, "quality_candy") },
		func(_ *Account, r map[string]any) { r["total"] = 1 },
		func(a *Account, _ map[string]any) { a.Extra[GatewayBorrowAccountQualityPendingKey] = true },
		func(_ *Account, r map[string]any) {
			r["latest_probe_at"] = now.Add(-25 * time.Hour).Format(time.RFC3339Nano)
		},
	} {
		a, record := accountQualityFixture(now, "healthy")
		mutate(a, record)
		candidate := priorityCandidate(1, .1, 0)
		candidate.account = a
		a.Concurrency = 10
		passed := true
		score := scorePriorityCandidate(DefaultPrioritySchedulingConfig(), candidate, PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500, QualityPassed: 20, QualitySamples: 20, LatestQualityPassed: &passed}, now)
		require.Equal(t, "insufficient", score.Tier)
		require.Nil(t, score.LatestQualityPassed)
		require.Contains(t, score.Reasons, "quality_unknown")
	}
}

func TestPriorityRequiredBorrowUsesVerifiedRouteAndRejectsMissingRoute(t *testing.T) {
	now := time.Now().UTC()
	svc, a, provider := borrowSafetyService()
	native, _ := accountQualityFixture(now, "degraded")
	a.Extra[GatewayBorrowAccountQualityModeKey] = GatewayBorrowAccountQualityMode
	a.Extra["quality_candy"] = native.Extra["quality_candy"]
	// The raw native record belongs to another identity and fails helper
	// validation: only the revision-bound route proof can qualify this account.
	item := openAIAccountCandidateScore{account: a, loadKnown: true, loadInfo: &AccountLoadInfo{AccountID: a.ID}}
	signal := PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500, QualityPassed: 0, QualitySamples: 20}
	cfg := DefaultPrioritySchedulingConfig()
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		expires := now.Add(time.Minute)
		provider.snapshot.Targets = []AstraRouteStatus{{AccountID: a.ID, Model: model, State: "ready", Reason: "target_probe_passed", ExpiresAt: &expires}}
		candidate := item
		svc.capturePriorityOAuthQuality(OpenAIAccountScheduleRequest{RequestedModel: model}, &candidate, now, 24*time.Hour)
		score := applyPriorityCandidate(cfg, &candidate, signal, now)
		require.NotNil(t, score.LatestQualityPassed)
		require.True(t, *score.LatestQualityPassed)
		require.NotContains(t, score.Reasons, "quality_below_target")
		require.False(t, candidate.priorityUnhealthy)
		provider.snapshot.Targets[0].State = "blocked"
		svc.capturePriorityOAuthQuality(OpenAIAccountScheduleRequest{RequestedModel: model}, &candidate, now, 24*time.Hour)
		score = applyPriorityCandidate(cfg, &candidate, signal, now)
		require.Nil(t, score.LatestQualityPassed)
		scheduler := &defaultOpenAIAccountScheduler{service: svc}
		allowed, reason := scheduler.isAccountRequestCompatibleReason(context.Background(), a, OpenAIAccountScheduleRequest{RequestedModel: model})
		require.False(t, allowed)
		require.Equal(t, "gateway_borrow_route_not_ready", reason)
	}
}

func TestPriorityOAuthSnapshotRetainsSanitizedQualityDecision(t *testing.T) {
	now := time.Now().UTC()
	a, _ := accountQualityFixture(now, "healthy")
	a.ID, a.Concurrency = 703, 10
	cfg := DefaultPrioritySchedulingConfig()
	item := openAIAccountCandidateScore{account: a, loadKnown: true, loadInfo: &AccountLoadInfo{AccountID: a.ID}}
	svc := &OpenAIGatewayService{}
	svc.capturePriorityOAuthQuality(OpenAIAccountScheduleRequest{RequestedModel: "gpt-6.1-sol"}, &item, now, 24*time.Hour)
	input := newPrioritySnapshotInput(OpenAIAccountScheduleRequest{RequestedModel: "gpt-6.1-sol"}, cfg, []openAIAccountCandidateScore{item}, now)
	projection := input.candidates[0]
	require.Nil(t, projection.account.Credentials)
	require.Nil(t, projection.account.Extra)
	require.NotNil(t, projection.priorityQualityPassed)
	require.True(t, *projection.priorityQualityPassed)
	*item.priorityQualityPassed = false
	require.True(t, *projection.priorityQualityPassed, "snapshot owns its decision")
	score := scorePriorityCandidate(cfg, projection, PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500}, now)
	require.Equal(t, "eligible", score.Tier)
	score = scorePriorityCandidate(cfg, projection, PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500}, projection.priorityQualityExpires)
	require.Nil(t, score.LatestQualityPassed, "dashboard cannot keep expired proof alive")
}

func TestOAuthProfitExemptForAllGroupsWithoutChangingStoredCosts(t *testing.T) {
	a := &Account{ID: 700, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{AccountCostMultiplierExtraKey: 10.0}}
	for _, group := range []int64{3, 5, 15, 16} {
		for _, threshold := range []float64{0, .001, 1, 100} {
			ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{groupID: group, threshold: threshold, platform: PlatformOpenAI})
			for _, rate := range []*float64{nil, floatPtr(-1), floatPtr(math.NaN()), floatPtr(1e9)} {
				a.RateMultiplier = rate
				vetoed, reason := OpenAIProfitControlVeto(ctx, a)
				require.False(t, vetoed)
				require.Empty(t, reason)
				latest, vetoed, reason := profitControlVetoLatest(ctx, a, nil)
				require.Same(t, a, latest)
				require.False(t, vetoed)
				require.Empty(t, reason)
				require.Equal(t, 10.0, a.CostMultiplier())
				require.Same(t, rate, a.RateMultiplier)
				verdict := previewAccountProfitAdmission(a, true, threshold, threshold/2, time.Now())
				require.Equal(t, ProfitPreviewClassAdmitted, verdict.Class)
				require.Equal(t, "not_applicable", verdict.RateSource)
				require.False(t, verdict.RejectedUnderMinD)
			}
		}
	}
	// Paid API keys retain the existing gate and missing-rate rejection.
	ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{threshold: .5})
	api := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, RateMultiplier: floatPtr(.8)}
	vetoed, reason := OpenAIProfitControlVeto(ctx, api)
	require.True(t, vetoed)
	require.Equal(t, openAIProfitFilterReasonThreshold, reason)
	api.RateMultiplier = nil
	vetoed, reason = OpenAIProfitControlVeto(ctx, api)
	require.True(t, vetoed)
	require.Equal(t, openAIProfitFilterReasonInvalidAccountRate, reason)
}

func TestPriorityEightyPercentIsBusyClassificationNotHardLimit(t *testing.T) {
	item := priorityCandidate(1, .1, 80)
	healthy := true
	item.account.Type, item.account.Status, item.account.Schedulable = AccountTypeOAuth, StatusActive, true
	cfg := DefaultPrioritySchedulingConfig()
	score := applyPriorityCandidate(cfg, &item, PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500, LatestQualityPassed: &healthy}, time.Now())
	require.Contains(t, score.Reasons, "busy")
	require.False(t, item.priorityUnhealthy)
	order := buildPrioritySelectionOrder([]openAIAccountCandidateScore{item}, OpenAIAccountScheduleRequest{})
	require.Len(t, order, 1, "busy accounts retain their actual remaining slots")
	acquired := []int64{}
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*item.account}}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, acquireResults: map[int64]bool{1: true}})}}
	selected, _, err := scheduler.tryAcquireOpenAISelectionOrder(context.Background(), OpenAIAccountScheduleRequest{}, order)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, []int64{1}, acquired)
	if selected.ReleaseFunc != nil {
		selected.ReleaseFunc()
	}
}

func TestPriorityAccountQualityScopeMapsOnceAndDoesNotInventLunaEvidence(t *testing.T) {
	now := time.Now().UTC()
	a, _ := accountQualityFixture(now, "healthy")
	a.Concurrency = 10
	a.Credentials["model_mapping"] = map[string]any{"public-sol": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6-luna", "gpt-6-luna": "gpt-6-luna"}
	cfg := DefaultPrioritySchedulingConfig()
	svc := &OpenAIGatewayService{}
	for _, tc := range []struct {
		model string
		known bool
	}{{"public-sol", true}, {"gpt-6.1-sol", false}, {"gpt-6-luna", false}} {
		item := openAIAccountCandidateScore{account: a, loadKnown: true, loadInfo: &AccountLoadInfo{}}
		svc.capturePriorityOAuthQuality(OpenAIAccountScheduleRequest{RequestedModel: tc.model}, &item, now, 24*time.Hour)
		passed := true
		score := scorePriorityCandidate(cfg, item, PrioritySchedulingSignal{Samples: 20, P90TTFTMs: 500, LatestQualityPassed: &passed}, now)
		require.Equal(t, tc.known, score.LatestQualityPassed != nil, tc.model)
		fallback := newPrioritySnapshotInput(OpenAIAccountScheduleRequest{RequestedModel: tc.model}, cfg, []openAIAccountCandidateScore{{account: a, loadKnown: true, loadInfo: &AccountLoadInfo{}}}, now)
		require.Equal(t, tc.known, fallback.candidates[0].priorityQualityPassed != nil, tc.model)
		if tc.known {
			require.Equal(t, now.Add(-time.Minute).Add(24*time.Hour), fallback.candidates[0].priorityQualityExpires, "copy actual observation expiry")
		}
	}
}
