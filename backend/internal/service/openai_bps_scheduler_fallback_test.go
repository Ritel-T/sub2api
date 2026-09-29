package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIBPSSchedulerFallsBackBeforeWaiting(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		bpsLoad              int
		bpsAcquired          bool
		nativeAcquired       bool
		nativeExcluded       bool
		protected            bool
		bpsDisabled          bool
		wantError            bool
		wantAccount          int64
		wantWait             bool
		subscriptionPriority bool
	}{
		{name: "native subscription does not bypass available BPS", subscriptionPriority: true, bpsAcquired: true, nativeAcquired: true, wantAccount: 1},
		{name: "native subscription can absorb full BPS", subscriptionPriority: true, bpsLoad: 100, nativeAcquired: true, wantAccount: 2},
		{name: "BPS stays preferred when both are idle", bpsAcquired: true, nativeAcquired: true, wantAccount: 1},
		{name: "full BPS falls back to idle native", bpsLoad: 100, nativeAcquired: true, wantAccount: 2},
		{name: "slot lost after load read falls back to native", nativeAcquired: true, wantAccount: 2},
		{name: "both busy still return a bounded wait", bpsLoad: 100, wantAccount: 1, wantWait: true},
		{name: "excluded native never bypasses policy", bpsLoad: 100, nativeAcquired: true, nativeExcluded: true, wantAccount: 1, wantWait: true},
		{name: "protected group waits for BPS rather than native", protected: true, bpsLoad: 100, nativeAcquired: true, wantAccount: 1, wantWait: true},
		{name: "protected group with BPS disabled refuses native", protected: true, bpsDisabled: true, nativeAcquired: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
					Extra: map[string]any{"openai_excel_bps": true}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
			}
			if tc.protected {
				accounts[0].GroupIDs = []int64{16}
				accounts[1].GroupIDs = []int64{16}
				accounts[0].Extra[ExcelBPSRequiredGroupIDsKey] = []int64{16}
				accounts[0].Extra[ExcelBPSRequiredModelsKey] = []string{"gpt-6-astra"}
				accounts[0].Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
				if tc.bpsDisabled {
					accounts[0].Extra["openai_excel_bps"] = false
				}
			}
			if tc.subscriptionPriority {
				accounts[1].Type = AccountTypeOAuth
				accounts[1].Credentials = map[string]any{"plan_type": "plus"}
			}
			acquired, released := []int64{}, []int64{}
			cache := schedulerTestConcurrencyCache{
				loadMap: map[int64]*AccountLoadInfo{
					1: {AccountID: 1, LoadRate: tc.bpsLoad, CurrentConcurrency: tc.bpsLoad / 100},
					2: {AccountID: 2},
				},
				acquireResults: map[int64]bool{1: tc.bpsAcquired, 2: tc.nativeAcquired},
				acquiredIDs:    &acquired, releasedIDs: &released,
			}
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.LBTopK = 1
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
				cfg:         cfg, concurrencyService: NewConcurrencyService(cache),
			}
			scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
			req := OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-6-astra", SubscriptionPriority: tc.subscriptionPriority}
			if tc.protected {
				groupID := int64(16)
				req.GroupID = &groupID
			}
			if tc.nativeExcluded {
				req.ExcludedIDs = map[int64]struct{}{2: {}}
			}
			selection, _, err := scheduler.Select(context.Background(), req)
			if tc.wantError {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, selection)
				require.NotContains(t, acquired, int64(2), "native account must not be acquired")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, tc.wantAccount, selection.Account.ID)
			if tc.wantWait {
				require.NotNil(t, selection.WaitPlan)
				require.False(t, selection.Acquired)
			} else {
				require.Nil(t, selection.WaitPlan)
				require.True(t, selection.Acquired)
				require.NotNil(t, selection.ReleaseFunc)
				selection.ReleaseFunc()
				require.Equal(t, []int64{tc.wantAccount}, released)
			}
			if tc.nativeExcluded || tc.protected {
				require.NotContains(t, acquired, int64(2))
			}
		})
	}
}

func TestOpenAIBPSProtectedGroupChannelMappingFiltersNative(t *testing.T) {
	groupID := int64(16)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			GroupIDs: []int64{groupID}, Extra: map[string]any{
				"openai_excel_bps":          true,
				"openai_excel_bps_models":   []string{"gpt-6-astra"},
				ExcelBPSRequiredGroupIDsKey: []int64{groupID},
				ExcelBPSRequiredModelsKey:   []string{"gpt-6-astra"},
			}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			GroupIDs: []int64{groupID}},
	}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cfg: &config.Config{}}
	ctx := WithOpenAIForwardModel(context.Background(), "gpt-6-astra", false)
	candidates, err := svc.listSchedulableAccountsForRequest(ctx, &groupID, PlatformOpenAI, "client-alias", false, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, int64(1), candidates[0].ID)
}
