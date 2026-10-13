package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func alphaBorrowSchedulerFixture(t *testing.T, advanced, protectedOnly bool) (*OpenAIGatewayService, *Account, int64) {
	t.Helper()
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	service, protected, provider := borrowSafetyService()
	groupID := int64(501)
	protected.Status, protected.Schedulable, protected.Concurrency = StatusActive, true, 1
	protected.GroupIDs = []int64{groupID}
	protected.Priority = 0
	native := *protected
	native.ID, native.Priority = 902, 100
	native.Extra = map[string]any{}
	accounts := []Account{*protected, native}
	if protectedOnly {
		accounts = accounts[:1]
	}
	service.accountRepo = schedulerTestOpenAIAccountRepo{accounts: accounts}
	service.cache = &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:alpha-sticky": protected.ID}}
	service.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
	service.cfg.Gateway.Scheduling.LoadBatchEnabled = false
	if advanced {
		service.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true", "false")
	}
	provider.snapshot.Targets[0].ExpiresAt = func() *time.Time { e := time.Now().Add(time.Minute); return &e }()
	return service, protected, groupID
}

func TestAlphaSearchBorrowReadyStickyAccountIsNotACompatibleEndpoint(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			svc, protected, group := alphaBorrowSchedulerFixture(t, advanced, false)
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(t.Context(), &group, "", "alpha-sticky", "gpt-6-astra", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityAlphaSearch, false, false, false)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.NotEqual(t, protected.ID, selection.Account.ID, "an IQ-ready borrowed Responses route cannot serve standalone alpha search")
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestAlphaSearchProtectedOnlyPoolHasNoCompatibleAccount(t *testing.T) {
	svc, _, group := alphaBorrowSchedulerFixture(t, false, true)
	selection, _, err := svc.SelectAccountWithSchedulerForCapability(t.Context(), &group, "", "alpha-sticky", "gpt-6-astra", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityAlphaSearch, false, false, false)
	require.Error(t, err)
	require.Nil(t, selection, "reject before any model/search network send")
}

func TestAlphaSearchBorrowCapabilityUsesActualChannelAndAccountMapping(t *testing.T) {
	for _, tc := range []struct {
		name, requested, forward string
		mapping                  map[string]any
		compatible               bool
	}{
		{"account_alias_to_astra", "public", "", map[string]any{"public": "gpt-6-astra"}, false},
		{"account_alias_to_luna", "public", "", map[string]any{"public": "gpt-6-luna"}, true},
		{"no_second_mapping_to_luna", "public", "", map[string]any{"public": "gpt-6.1-sol", "gpt-6.1-sol": "gpt-6-luna"}, false},
		{"no_second_mapping_to_astra", "public", "", map[string]any{"public": "gpt-6-luna", "gpt-6-luna": "gpt-6-astra"}, true},
		{"channel_alias_to_astra", "public", "gpt-6-astra", map[string]any{}, false},
		{"channel_then_account_to_astra", "public", "channel-alias", map[string]any{"channel-alias": "gpt-6-astra"}, false},
		{"channel_then_account_to_luna", "gpt-6-astra", "channel-alias", map[string]any{"channel-alias": "gpt-6-luna"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, account, _ := borrowSafetyService()
			mapping := map[string]any{tc.requested: tc.requested}
			for key, value := range tc.mapping {
				mapping[key] = value
			}
			account.Credentials["model_mapping"] = mapping
			ctx := context.Context(t.Context())
			if tc.forward != "" {
				ctx = WithOpenAIForwardModel(ctx, tc.forward, false)
			}
			scheduler, ok := newDefaultOpenAIAccountScheduler(svc, nil).(*defaultOpenAIAccountScheduler)
			require.True(t, ok)
			compatible, _ := scheduler.isAccountRequestCompatibleReason(ctx, account, OpenAIAccountScheduleRequest{RequestedModel: tc.requested, RequiredCapability: OpenAIEndpointCapabilityAlphaSearch, RequiredTransport: OpenAIUpstreamTransportHTTPSSE})
			require.Equal(t, tc.compatible, compatible)
		})
	}
}

func TestAlphaSearchBorrowFixKeepsResponsesAndNativeEndpointCapabilities(t *testing.T) {
	svc, account, group := alphaBorrowSchedulerFixture(t, false, true)
	selection, _, err := svc.SelectAccountWithSchedulerForCapability(t.Context(), &group, "", "alpha-sticky", "gpt-6-astra", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityResponses, false, false, false)
	require.NoError(t, err)
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		native := &Account{Platform: PlatformOpenAI, Type: kind}
		require.True(t, accountSupportsOpenAICapabilitiesForRequest(native, "gpt-6-astra", OpenAIEndpointCapabilityAlphaSearch, ""))
	}
	pat := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "at-fixture", openAIAuthModeCredentialKey: OpenAIAuthModePersonalAccessToken}}
	require.True(t, pat.IsOpenAIPersonalAccessToken())
	require.True(t, accountSupportsOpenAICapabilitiesForRequest(pat, "gpt-6-astra", OpenAIEndpointCapabilityAlphaSearch, ""))
	passthrough := *account
	passthrough.Extra = map[string]any{GatewayBorrowModelsKey: []string{"gpt-6-astra"}, "openai_passthrough": true}
	passthrough.Credentials = map[string]any{"model_mapping": map[string]any{"public": "gpt-6-astra"}}
	require.False(t, accountSupportsOpenAICapabilitiesForRequest(&passthrough, "public", OpenAIEndpointCapabilityAlphaSearch, ""), "alpha still account-maps exactly once for passthrough accounts")
	// The send-time guard remains authoritative if selection state has changed.
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, chatgptCodexAlphaSearchURL, strings.NewReader(`{"model":"gpt-6-astra"}`))
	request.Header.Set("Authorization", "Bearer "+account.GetOpenAIAccessToken())
	_, err = svc.admitOpenAIHTTPRequest(request, account)
	require.ErrorContains(t, err, "gateway_borrow_endpoint_unavailable")
}

func TestAlphaSearchMappedForwardModelControlsStickyEligibility(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, forward := range []string{"gpt-6-astra", "gpt-6-luna"} {
			t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced]+"/"+forward, func(t *testing.T) {
				svc, protected, group := alphaBorrowSchedulerFixture(t, advanced, false)
				ctx := WithOpenAIForwardModel(t.Context(), forward, false)
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &group, "", "alpha-sticky", "gpt-6-astra", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityAlphaSearch, false, false, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				if forward == "gpt-6-astra" {
					require.NotEqual(t, protected.ID, selection.Account.ID)
				} else {
					require.Equal(t, protected.ID, selection.Account.ID, "a model that uses this account's native path still supports alpha search")
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			})
		}
	}
}

func TestAlphaSearchInitialQualityPendingUsesActualWireModel(t *testing.T) {
	for _, tc := range []struct {
		requested, forward string
		compatible         bool
	}{
		{"gpt-6-astra", "", false},
		{"gpt-6.1-sol", "", false},
		{"gpt-6-luna", "", true},
		{"gpt-6-luna", "gpt-6-astra", false},
		{"gpt-6-astra", "gpt-6-luna", true},
	} {
		t.Run(tc.requested+"/"+tc.forward, func(t *testing.T) {
			account := pendingAccountFixture()
			ctx := context.Context(t.Context())
			if tc.forward != "" {
				ctx = WithOpenAIForwardModel(ctx, tc.forward, false)
			}
			svc := &OpenAIGatewayService{}
			scheduler, ok := newDefaultOpenAIAccountScheduler(svc, nil).(*defaultOpenAIAccountScheduler)
			require.True(t, ok)
			advanced, _ := scheduler.isAccountRequestCompatibleReason(ctx, account, OpenAIAccountScheduleRequest{RequestedModel: tc.requested, RequiredCapability: OpenAIEndpointCapabilityAlphaSearch, RequiredTransport: OpenAIUpstreamTransportHTTPSSE})
			require.Equal(t, tc.compatible, advanced)
			legacyReason := openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, account, nil, PlatformOpenAI, tc.requested, false, OpenAIEndpointCapabilityAlphaSearch)
			require.Equal(t, tc.compatible, legacyReason == "")
		})
	}
}
