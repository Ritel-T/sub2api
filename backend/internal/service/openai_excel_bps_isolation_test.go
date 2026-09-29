package service

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func isolatedExcelAccount() *Account {
	a := excelAccount()
	a.GroupIDs = []int64{1, 15, 16, 19}
	a.Status = StatusActive
	a.Schedulable = true
	a.Extra[ExcelBPSRequiredGroupIDsKey] = []any{float64(16)}
	a.Extra[ExcelBPSRequiredModelsKey] = []any{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-astra"}
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-astra"}
	return a
}

func TestExcelBPSIsolationGroupAndModelPolicy(t *testing.T) {
	account := isolatedExcelAccount()
	account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
	for _, group := range []int64{0, 1, 15, 16, 19} {
		for _, model := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol-high", "gpt-6-sol", "gpt-6-astra", "gpt-6-astra-high", "openai/gpt-6-astra", "alias", "gpt-6-luna", "gpt-5.6-sol", "future-model"} {
			t.Run(fmt.Sprintf("%d/%s", group, model), func(t *testing.T) {
				protected := model != "gpt-6-luna" && model != "gpt-5.6-sol" && model != "future-model"
				require.Equal(t, !protected || group == 16, account.IsModelAllowedInGroup(&group, model))
				require.Equal(t, protected, account.IsExcelBPSEnabledForModel(model))
			})
		}
	}
	require.False(t, account.IsModelAllowedInGroup(nil, "gpt-6-astra"))
	require.True(t, account.IsModelAllowedInGroup(nil, "gpt-6-luna"))
	account.Extra[ExcelBPSRequiredGroupIDsKey] = []any{}
	group := int64(1)
	require.True(t, account.IsModelAllowedInGroup(&group, "gpt-6-astra"), "clearing the isolation restores the existing account policy")
}

func TestExcelBPSIsolationDisabledOrMissingBPSNeverBecomesNative(t *testing.T) {
	for _, mutation := range []string{"disabled", "models_missing", "models_empty", "models_wrong", "models_all"} {
		t.Run(mutation, func(t *testing.T) {
			a := isolatedExcelAccount()
			switch mutation {
			case "disabled":
				a.Extra["openai_excel_bps"] = false
			case "models_missing":
				delete(a.Extra, "openai_excel_bps_models")
			case "models_empty":
				a.Extra["openai_excel_bps_models"] = []any{}
			case "models_wrong":
				a.Extra["openai_excel_bps_models"] = []any{"gpt-6-luna"}
			case "models_all":
				a.Extra["openai_excel_bps_models"] = nil
			}
			for _, group := range []int64{1, 15, 16, 19} {
				require.False(t, a.IsModelAllowedInGroup(&group, "gpt-6-astra"))
				require.True(t, a.IsModelAllowedInGroup(&group, "gpt-6-luna"))
				require.False(t, a.IsExcelBPSEnabledForModel("gpt-6-luna"))
			}
		})
	}
}

func TestExcelBPSIsolationSchedulersHonorChannelMapping(t *testing.T) {
	a := isolatedExcelAccount()
	svc := &OpenAIGatewayService{}
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	for _, group := range []int64{1, 15, 16, 19} {
		ctx := WithOpenAIForwardModel(context.Background(), "gpt-6-astra", false)
		req := OpenAIAccountScheduleRequest{GroupID: &group, RequestedModel: "gpt-6-luna"}
		compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, a, req)
		legacy := openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, a, &group, PlatformOpenAI, "gpt-6-luna", false, OpenAIEndpointCapabilityResponses)
		if group == 16 {
			require.True(t, compatible, reason)
			require.Empty(t, legacy)
		} else {
			require.False(t, compatible)
			require.Equal(t, "model_not_allowed_in_group", reason)
			require.Equal(t, "model_not_allowed_in_group", legacy)
		}
	}
}

func TestExcelBPSIsolationStickyAndPreviousOwners(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, kind := range []string{"pool", "sticky", "required_owner", "movable_owner"} {
			t.Run(engine+"/"+kind, func(t *testing.T) {
				accounts := encryptedMessageCapabilityAccounts()
				accounts[0].Extra = isolatedExcelAccount().Extra
				group := int64(1)
				for i := range accounts {
					accounts[i].GroupIDs = []int64{group}
				}
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				ctx := context.Background()
				session, previous := "", ""
				if kind == "sticky" {
					session = "isolation-sticky"
					require.NoError(t, svc.setStickySessionAccountID(ctx, &group, session, accounts[0].ID, time.Hour))
				}
				if strings.HasSuffix(kind, "owner") {
					previous = "resp_isolation_owner"
					require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, group, previous, accounts[0].ID, time.Hour))
				}
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &group, previous, session, "gpt-6-astra", nil, OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, kind == "movable_owner", false)
				if kind == "required_owner" {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Nil(t, selection)
				} else {
					require.NoError(t, err)
					require.NotNil(t, selection)
					require.Equal(t, accounts[1].ID, selection.Account.ID)
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
				}
				require.NotContains(t, acquired, accounts[0].ID)
				require.ElementsMatch(t, acquired, released)
			})
		}
	}
}

func isolationForwardContext(group int64, path string) (*gin.Context, *httptest.ResponseRecorder) {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set("api_key", &APIKey{GroupID: &group})
	return c, r
}

func TestExcelBPSIsolationForwardRoutes(t *testing.T) {
	for _, group := range []int64{1, 15, 16, 19} {
		for _, model := range []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna"} {
			t.Run(fmt.Sprintf("%d/%s", group, model), func(t *testing.T) {
				wire := fmt.Sprintf("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_isolation\",\"status\":\"completed\",\"model\":%q,\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", model)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
				svc := openAIClientToolsTestService(upstream)
				a := isolatedExcelAccount()
				c, _ := isolationForwardContext(group, "/v1/responses")
				_, err := svc.Forward(context.Background(), c, a, []byte(fmt.Sprintf(`{"model":%q,"input":"test","stream":true}`, model)))
				if model != "gpt-6-luna" && group != 16 {
					require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
					require.Empty(t, upstream.requests)
					return
				}
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				if model == "gpt-6-luna" {
					require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
				} else {
					require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
				}
			})
		}
	}
}

func TestExcelBPSIsolationNativeOnlyRequestsCannotFallback(t *testing.T) {
	for _, tools := range []string{`"tools":[{"type":"image_generation"}]`, `"tools":[{"type":"web_search","external_web_access":true}]`, `"tool_choice":{"type":"web_search"}`} {
		upstream := &httpUpstreamRecorder{}
		svc := openAIClientToolsTestService(upstream)
		a := isolatedExcelAccount()
		c, r := isolationForwardContext(16, "/v1/responses")
		body := []byte(`{"model":"gpt-6-astra","input":"test",` + tools + `}`)
		_, err := svc.Forward(context.Background(), c, a, body)
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, r.Code)
		require.Contains(t, r.Body.String(), "basispoints_native_fallback_disabled")
		require.Empty(t, upstream.requests)
		a.Extra[excelBPSCooldownTestKey] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), body)
		require.True(t, svc.isOpenAIExcelBPSCooldownBlocked(ctx, a, "gpt-6-astra"), "native-only input cannot bypass the BPS cooldown")
	}
}

func TestExcelBPSIsolationCompatIngressCannotUseNative(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			svc := openAIClientToolsTestService(upstream)
			c, r := isolationForwardContext(16, path)
			body := []byte(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"test"}]}`)
			var err error
			if path == "/v1/messages" {
				_, err = svc.ForwardAsAnthropic(context.Background(), c, isolatedExcelAccount(), body, "", "")
			} else {
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, isolatedExcelAccount(), body, "", "")
			}
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, r.Code)
			require.Contains(t, r.Body.String(), "use /v1/responses")
			require.Empty(t, upstream.requests)
		})
	}
}

func TestExcelBPSIsolationLateAdmissionAndWS(t *testing.T) {
	a := isolatedExcelAccount()
	latest := *a
	latest.Extra = maps.Clone(a.Extra)
	svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: &latest}}
	latest.Extra[ExcelBPSRequiredGroupIDsKey] = []any{float64(19)}
	_, err := svc.admitOpenAITurnForGroup(context.Background(), 16, a, "gpt-6-astra")
	require.True(t, IsOpenAITurnAdmissionError(err))
	require.NotEqual(t, openAITurnRouteFingerprint(a), openAITurnRouteFingerprint(&latest))
	require.Error(t, svc.checkOpenAIWSBinding(a, "gpt-6-astra", svc.bindOpenAIWSHandshake(a, "gpt-6-astra", nil)))
	ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(`{"input":[{"role":"assistant","content":[{"type":"encrypted_content","encrypted_content":"opaque"}]}]}`))
	require.True(t, openAIEncryptedMessageCapabilityMismatch(ctx, a, "gpt-6-astra"))
	require.False(t, openAIEncryptedMessageCapabilityMismatch(ctx, a, "gpt-6-luna"))
}

func TestExcelBPSIsolationCompactMappingCannotBypassProtocol(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := openAIClientToolsTestService(upstream)
	a := isolatedExcelAccount()
	a.Extra["openai_passthrough"] = true
	a.Credentials["compact_model_mapping"] = map[string]any{"gpt-6-luna": "gpt-6-astra"}
	c, _ := isolationForwardContext(15, "/v1/responses/compact")
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
	_, err := svc.Forward(context.Background(), c, a, []byte(`{"model":"gpt-6-luna","input":[{"role":"user","content":"test"}]}`))
	require.Error(t, err)
	require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
	require.Empty(t, upstream.requests, "compact remapping cannot emit a protected model on Codex")
}

func TestExcelBPSIsolationValidation(t *testing.T) {
	a := isolatedExcelAccount()
	require.NoError(t, validateExcelBPSGroupIsolationExtra(a.Extra))
	for _, ids := range []any{"16", []any{0}, []any{-1}, []any{16.5}} {
		a.Extra[ExcelBPSRequiredGroupIDsKey] = ids
		require.Error(t, validateExcelBPSGroupIsolationExtra(a.Extra))
	}
	a.Extra[ExcelBPSRequiredGroupIDsKey] = []int64{16}
	delete(a.Extra, ExcelBPSRequiredModelsKey)
	require.Error(t, validateExcelBPSGroupIsolationExtra(a.Extra))
	a.Extra[ExcelBPSRequiredGroupIDsKey] = nil
	require.NoError(t, validateExcelBPSGroupIsolationExtra(a.Extra))
}
