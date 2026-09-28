package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const excelBPSCooldownTestKey = "openai_excel_bps_rate_limit_reset_at"

func excelBPSCooldownTestAccounts(until time.Time) []Account {
	accounts := encryptedMessageCapabilityAccounts()
	accounts[0].Extra[excelBPSCooldownTestKey] = until.UTC().Format(time.RFC3339)
	return accounts
}

func TestExcelBPSCooldownScheduling_RouteAndExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, body, model, forward       string
		omit, expired, invalid, disabled bool
		want                             bool
	}{
		{name: "bps_model", body: `{}`, model: "gpt-6-astra", want: true},
		{name: "native_model", body: `{}`, model: "gpt-6-sol"},
		{name: "account_alias", body: `{}`, model: "client-alias", want: true},
		{name: "channel_then_account_alias", body: `{}`, model: "gpt-6-sol", forward: "client-alias", want: true},
		{name: "channel_native_model", body: `{}`, model: "gpt-6-astra", forward: "gpt-6-sol"},
		{name: "native_image_tool", body: `{"tools":[{"type":"image_generation"}]}`, model: "gpt-6-astra", want: true},
		{name: "omit_image_tool", body: `{"tools":[{"type":"image_generation"}]}`, model: "gpt-6-astra", omit: true, want: true},
		{name: "native_search", body: `{"tools":[{"type":"web_search","external_web_access":true}]}`, model: "gpt-6-astra", want: true},
		{name: "native_search_high", body: `{"tools":[{"type":"web_search","search_context_size":"high"}]}`, model: "gpt-6-astra", want: true},
		{name: "omit_search", body: `{"tools":[{"type":"web_search","external_web_access":true}]}`, model: "gpt-6-astra", omit: true, want: true},
		{name: "tool_choice_none_stays_bps", body: `{"tools":[{"type":"image_generation"}],"tool_choice":"none"}`, model: "gpt-6-astra", want: true},
		{name: "forced_native_tool", body: `{"tool_choice":{"type":"image_generation"}}`, model: "gpt-6-astra", want: true},
		{name: "inline_image_stays_bps", body: `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,synthetic"}]}]}`, model: "gpt-6-astra", want: true},
		{name: "expired", body: `{}`, model: "gpt-6-astra", expired: true},
		{name: "invalid_timestamp", body: `{}`, model: "gpt-6-astra", invalid: true},
		{name: "bps_disabled", body: `{}`, model: "gpt-6-astra", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			until := time.Now().Add(time.Hour)
			if tc.expired {
				until = time.Now().Add(-time.Second)
			}
			account := excelBPSCooldownTestAccounts(until)[0]
			account.Credentials = map[string]any{"model_mapping": map[string]any{"client-alias": "gpt-6-astra", "gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6-sol"}}
			account.Extra[ExcelBPSOmitUnsupportedToolsKey] = tc.omit
			if tc.invalid {
				account.Extra[excelBPSCooldownTestKey] = "invalid"
			}
			if tc.disabled {
				account.Extra["openai_excel_bps"] = false
			}
			before, err := json.Marshal(account)
			require.NoError(t, err)
			svc := &OpenAIGatewayService{}
			ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(tc.body))
			if tc.forward != "" {
				ctx = WithOpenAIForwardModel(ctx, tc.forward, false)
			}
			require.Equal(t, tc.want, svc.isOpenAIExcelBPSCooldownBlocked(ctx, &account, tc.model))
			require.Equal(t, tc.want, isOpenAIExcelBPSCooldownBlocked(svc.withExcelBPSCooldownContext(ctx), &account, tc.model))
			if tc.want {
				require.Equal(t, openAIExcelBPSCooldownReason, openAICompatibleAccountEligibilityFailureReasonBeforeProfit(svc.withExcelBPSCooldownContext(ctx), &account, nil, PlatformOpenAI, tc.model, false, OpenAIEndpointCapabilityResponses))
			}
			after, err := json.Marshal(account)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
			require.True(t, account.IsSchedulable(), "BPS cooldown must not become an account-wide block")
			require.Nil(t, account.RateLimitResetAt)
		})
	}
}

func TestExcelBPSCooldownScheduling_LocalDeadlineAndContext(t *testing.T) {
	account := excelBPSCooldownTestAccounts(time.Now().Add(-time.Hour))[0]
	svc := &OpenAIGatewayService{}
	svc.excelBPSRateLimits.Store(account.ID, time.Now().Add(time.Hour))
	ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(`{"tools":[{"type":"image_generation"}]}`))
	ctx = svc.withExcelBPSCooldownContext(ctx)
	require.True(t, isOpenAIExcelBPSCooldownBlocked(ctx, &account, "gpt-6-astra"), "hosted tools stay on BPS")
	ctx = WithOpenAIResponsesRequestCapabilities(ctx, []byte(`{}`))
	require.True(t, isOpenAIExcelBPSCooldownBlocked(ctx, &account, "gpt-6-astra"), "new request must replace the old structural route decision")
	require.False(t, isOpenAIExcelBPSCooldownBlocked(ctx, &account, "gpt-6-sol"))
	require.False(t, isOpenAIExcelBPSCooldownBlocked(ctx, nil, "gpt-6-astra"))
	svc.excelBPSRateLimits.Store(account.ID, time.Now().Add(-time.Second))
	require.False(t, isOpenAIExcelBPSCooldownBlocked(ctx, &account, "gpt-6-astra"))
}

func TestExcelBPSCooldownScheduling_Engines(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, tc := range []struct {
			name                                             string
			local, sticky, native, expired, allBlocked, omit bool
			want                                             int64
		}{
			{name: "durable", want: 91002},
			{name: "durable_sticky", sticky: true, want: 91002},
			{name: "local", local: true, want: 91002},
			{name: "local_sticky", local: true, sticky: true, want: 91002},
			{name: "native_sticky", native: true, sticky: true, want: 91002},
			{name: "local_native_sticky", local: true, native: true, sticky: true, want: 91002},
			{name: "omit_native_tool", native: true, omit: true, sticky: true, want: 91002},
			{name: "expired_sticky", expired: true, sticky: true, want: 91001},
			{name: "all_blocked", allBlocked: true, sticky: true},
		} {
			t.Run(engine+"/"+tc.name, func(t *testing.T) {
				until := time.Now().Add(time.Hour)
				if tc.expired {
					until = time.Now().Add(-time.Second)
				}
				accounts := excelBPSCooldownTestAccounts(until)
				accounts[0].Extra[ExcelBPSOmitUnsupportedToolsKey] = tc.omit
				if tc.local {
					delete(accounts[0].Extra, excelBPSCooldownTestKey)
				}
				if tc.allBlocked {
					accounts[1].Extra = accounts[0].Extra
				}
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				if tc.local {
					svc.excelBPSRateLimits.Store(accounts[0].ID, until)
				}
				body := `{"model":"gpt-6-astra"}`
				if tc.native {
					body = `{"model":"gpt-6-astra","tools":[{"type":"image_generation"}]}`
				}
				ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(body))
				session := ""
				if tc.sticky {
					session = "bps-cooldown-sticky"
					require.NoError(t, svc.setStickySessionAccountID(ctx, nil, session, accounts[0].ID, time.Hour))
				}
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", session, "gpt-6-astra", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
				if tc.want == 0 {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Nil(t, selection)
					require.Empty(t, acquired)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, tc.want, selection.Account.ID)
				if tc.want == 91002 {
					require.NotContains(t, acquired, int64(91001), "reject before TopK and slot acquisition")
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				require.ElementsMatch(t, acquired, released)
			})
		}
	}
}

func TestExcelBPSCooldownScheduling_StaleSnapshot(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, sticky := range []bool{false, true} {
			name := engine + "/pool"
			if sticky {
				name = engine + "/sticky"
			}
			t.Run(name, func(t *testing.T) {
				fresh := excelBPSCooldownTestAccounts(time.Now().Add(time.Hour))
				fresh[1].Extra["openai_excel_bps"] = true
				fresh[1].Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
				stale := encryptedMessageCapabilityAccounts()[0]
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, fresh, &acquired, &released)
				svc.cfg.Gateway.OpenAIWS.LBTopK = 2
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{snapshotAccounts: []*Account{&stale, &fresh[1]}, accountsByID: map[int64]*Account{stale.ID: &stale, fresh[1].ID: &fresh[1]}}}
				ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(`{}`))
				require.Nil(t, svc.recheckSelectedOpenAIAccountFromDB(ctx, &stale, nil, PlatformOpenAI, "gpt-6-astra", false, OpenAIEndpointCapabilityResponses))
				session := ""
				if sticky {
					session = "bps-stale-snapshot"
					require.NoError(t, svc.setStickySessionAccountID(ctx, nil, session, stale.ID, time.Hour))
				}
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", session, "gpt-6-astra", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, fresh[1].ID, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				require.ElementsMatch(t, acquired, released)
			})
		}
	}
}

func TestExcelBPSCooldownScheduling_PreviousResponseOwner(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, tc := range []struct {
			name                   string
			canMove, native, local bool
		}{
			{name: "required"}, {name: "movable", canMove: true}, {name: "native_required", native: true}, {name: "local_required", local: true},
		} {
			t.Run(engine+"/"+tc.name, func(t *testing.T) {
				until := time.Now().Add(time.Hour)
				accounts := excelBPSCooldownTestAccounts(until)
				accounts[1].Extra["openai_excel_bps"] = true
				accounts[1].Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
				if tc.local {
					delete(accounts[0].Extra, excelBPSCooldownTestKey)
				}
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				if tc.local {
					svc.excelBPSRateLimits.Store(accounts[0].ID, until)
				}
				stale := encryptedMessageCapabilityAccounts()[0]
				// The stale snapshot has no deadline until its DB recheck; retain
				// both candidates inside the existing bounded selection window.
				svc.cfg.Gateway.OpenAIWS.LBTopK = 2
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{snapshotAccounts: []*Account{&stale, &accounts[1]}, accountsByID: map[int64]*Account{stale.ID: &stale, accounts[1].ID: &accounts[1]}}}
				body := `{}`
				if tc.native {
					body = `{"tools":[{"type":"image_generation"}]}`
				}
				ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(body))
				store := svc.getOpenAIWSStateStore()
				require.NoError(t, store.BindResponseAccount(ctx, 0, "resp_bps_cooldown_owner", accounts[0].ID, time.Hour))
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "resp_bps_cooldown_owner", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, tc.canMove, false)
				if !tc.canMove {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Contains(t, err.Error(), openAIExcelBPSCooldownReason)
					require.Nil(t, selection)
					require.Empty(t, acquired)
				} else {
					require.NoError(t, err)
					require.NotNil(t, selection)
					want := accounts[1].ID
					if tc.native {
						want = accounts[0].ID
					}
					require.Equal(t, want, selection.Account.ID)
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
				}
				owner, err := store.GetResponseAccount(ctx, 0, "resp_bps_cooldown_owner")
				require.NoError(t, err)
				require.Equal(t, accounts[0].ID, owner)
				require.ElementsMatch(t, acquired, released)
			})
		}
	}
}

func TestExcelBPSCooldownScheduling_PreDispatchFreshRead(t *testing.T) {
	fresh := excelBPSCooldownTestAccounts(time.Now().Add(time.Hour))
	stale := encryptedMessageCapabilityAccounts()[0]
	svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: &fresh[0]}}
	ctx := WithOpenAIForwardModel(context.Background(), "gpt-6-sol", false)
	err := svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-astra"}`))
	var cooldown *ExcelBPSCooldownError
	require.ErrorAs(t, err, &cooldown)
	require.False(t, IsOpenAITurnAdmissionError(err), "a BPS deadline must remain eligible for scoped failover, not generic admission 503")
	require.True(t, cooldown.ResetAt.After(time.Now()))
	require.NoError(t, svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-sol"}`)))
	err = svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-astra","tools":[{"type":"image_generation"}]}`))
	require.ErrorAs(t, err, &cooldown, "hosted tools cannot bypass BPS cooldown")
	stale.Extra[ExcelBPSOmitUnsupportedToolsKey] = true
	err = svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-astra","tools":[{"type":"image_generation"}]}`))
	require.True(t, IsOpenAITurnAdmissionError(err), "a changed fallback policy must not dispatch a previously selected BPS route")
	delete(stale.Extra, ExcelBPSOmitUnsupportedToolsKey)
	svc.accountRepo = &turnAdmissionRepo{err: errors.New("synthetic read failure")}
	err = svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-astra"}`))
	require.Error(t, err, "unavailable authoritative state must prevent dispatch")
	require.False(t, errors.As(err, &cooldown))
	svc.accountRepo = schedulerTestOpenAIAccountRepo{}
	svc.requireLatestTurnAdmission = true
	require.Error(t, svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-astra"}`)))
	svc.requireLatestTurnAdmission = false
	svc.excelBPSRateLimits.Store(stale.ID, time.Now().Add(time.Hour))
	err = svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-astra"}`))
	require.ErrorAs(t, err, &cooldown, "fixture repositories with embedded nil methods must use the supplied account and local deadline")
	require.NoError(t, svc.checkExcelBPSCooldownBeforeDispatch(ctx, &stale, []byte(`{"model":"gpt-6-sol"}`)))
}

type excelBPSCooldownOwnerRaceRepo struct {
	schedulerTestOpenAIAccountRepo
	stale *Account
	reads int
}

func (r *excelBPSCooldownOwnerRaceRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.reads++
	if r.reads == 1 {
		return r.stale, nil
	}
	return r.schedulerTestOpenAIAccountRepo.GetByID(ctx, id)
}

func TestExcelBPSCooldownScheduling_OwnerCooldownAfterPrecheck(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		t.Run(engine, func(t *testing.T) {
			accounts := excelBPSCooldownTestAccounts(time.Now().Add(time.Hour))
			stale := encryptedMessageCapabilityAccounts()[0]
			var acquired, released []int64
			svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
			svc.accountRepo = &excelBPSCooldownOwnerRaceRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, stale: &stale}
			svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{snapshotAccounts: []*Account{&stale, &accounts[1]}, accountsByID: map[int64]*Account{stale.ID: &stale, accounts[1].ID: &accounts[1]}}}
			ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(`{}`))
			store := svc.getOpenAIWSStateStore()
			require.NoError(t, store.BindResponseAccount(ctx, 0, "resp_bps_owner_race", stale.ID, time.Hour))
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "resp_bps_owner_race", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Contains(t, err.Error(), openAIExcelBPSCooldownReason)
			require.Nil(t, selection)
			require.Empty(t, acquired, "late cooldown must propagate as an error, not a load-balance miss")
			owner, err := store.GetResponseAccount(ctx, 0, "resp_bps_owner_race")
			require.NoError(t, err)
			require.Equal(t, stale.ID, owner)
		})
	}
}
