package service

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

const encryptedMessageCapabilityBody = `{"model":"gpt-6-astra","input":[{"type":"agent_message","content":[{"type":"input_text","text":"test wrapper"},{"type":"encrypted_content","encrypted_content":"synthetic-test-marker"}]}]}`

func TestOpenAIEncryptedMessageCapabilities_Detection(t *testing.T) {
	opaque, err := json.Marshal(map[string]any{"input": []any{
		map[string]any{"type": "custom_tool_call", "input": encryptedMessageCapabilityBody},
		map[string]any{"type": "function_call", "arguments": encryptedMessageCapabilityBody},
		map[string]any{"type": "function_call_output", "output": encryptedMessageCapabilityBody},
	}})
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"agent_message", encryptedMessageCapabilityBody, true},
		{"shorthand_user", `{"input":[{"role":"user","content":[{"type":"encrypted_content"}]}]}`, true},
		{"shorthand_assistant", `{"input":[{"role":"assistant","content":[{"type":"encrypted_content"}]}]}`, true},
		{"shorthand_system", `{"input":[{"role":"system","content":[{"type":"encrypted_content"}]}]}`, true},
		{"shorthand_developer", `{"input":[{"role":"developer","content":[{"type":"encrypted_content"}]}]}`, true},
		{"unknown_role", `{"input":[{"role":"tool","content":[{"type":"encrypted_content"}]}]}`, false},
		{"no_role_or_type", `{"input":[{"content":[{"type":"encrypted_content"}]}]}`, false},
		{"tool_with_role", `{"input":[{"type":"function_call","role":"user","arguments":{},"content":[{"type":"encrypted_content"}]}]}`, false},
		{"message", `{"input":[{"type":"message","role":"user","content":[{"type":"encrypted_content"}]}]}`, true},
		{"later_message", `{"input":[{"type":"message","content":[{"type":"input_text","text":"test"}]},{"type":"agent_message","content":[{"type":"encrypted_content"}]}]}`, true},
		{"plaintext", `{"input":[{"type":"agent_message","content":[{"type":"input_text","text":"encrypted_content"}]}]}`, false},
		{"reasoning", `{"input":[{"type":"reasoning","encrypted_content":"synthetic-test-marker"}]}`, false},
		{"reasoning_content", `{"input":[{"type":"reasoning","content":[{"type":"encrypted_content"}]}]}`, false},
		{"tool_result_object", `{"input":[{"type":"function_call_output","output":{"type":"message","content":[{"type":"encrypted_content"}]}}]}`, false},
		{"opaque_tool_json", string(opaque), false},
		{"nested_text_metadata", `{"input":[{"type":"message","content":[{"type":"input_text","metadata":{"type":"encrypted_content"}}]}]}`, false},
		{"content_not_array", `{"input":[{"type":"message","content":{"type":"encrypted_content"}}]}`, false},
		{"input_not_array", `{"input":{"type":"message","content":[{"type":"encrypted_content"}]}}`, false},
		{"empty", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), body)
			require.Equal(t, tc.want, openAIRequiresEncryptedMessageContent(ctx))
			require.Equal(t, tc.body, string(body), "classification must not rewrite history")
		})
	}
}

func TestOpenAIEncryptedMessageCapabilities_ContextAndModels(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := []byte(encryptedMessageCapabilityBody)
	ctx := WithOpenAIResponsesRequestCapabilities(parent, body)
	// Changing a later normalized body cannot erase the original requirement.
	body[0] = ' '
	req := httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	retryCtx := withOpenAIProxyStreamQuarantineBypass(req.Context())
	retryCtx, retryCancel := context.WithTimeout(retryCtx, time.Minute)
	defer retryCancel()
	require.True(t, openAIRequiresEncryptedMessageContent(retryCtx))
	cancel()
	require.ErrorIs(t, retryCtx.Err(), context.Canceled)

	bps := encryptedMessageCapabilityAccounts()[0]
	bps.Credentials = map[string]any{"model_mapping": map[string]any{"client-alias": "gpt-6-astra"}}
	require.True(t, openAIEncryptedMessageCapabilityMismatch(ctx, &bps, "client-alias"))
	require.False(t, openAIEncryptedMessageCapabilityMismatch(ctx, &bps, "gpt-6-sol"))
	require.True(t, openAIEncryptedMessageCapabilityMismatch(WithOpenAIForwardModel(ctx, "client-alias", false), &bps, "gpt-6-sol"))
	require.False(t, openAIEncryptedMessageCapabilityMismatch(WithOpenAIForwardModel(ctx, "gpt-6-sol", false), &bps, "client-alias"))
	require.False(t, openAIEncryptedMessageCapabilityMismatch(context.Background(), &bps, "client-alias"))
	require.False(t, openAIEncryptedMessageCapabilityMismatch(ctx, nil, "gpt-6-astra"))
	require.False(t, openAIRequiresEncryptedMessageContent(nil))
	require.False(t, openAIRequiresEncryptedMessageContent(WithOpenAIResponsesRequestCapabilities(nil, []byte(`{}`))))
	// A separate request is classified independently, even if it inherits a context.
	require.False(t, openAIRequiresEncryptedMessageContent(WithOpenAIResponsesRequestCapabilities(ctx, []byte(`{}`))))
}

func encryptedMessageCapabilityAccounts() []Account {
	return []Account{
		{
			ID: 91001, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
			Extra: map[string]any{
				"openai_excel_bps":                             true,
				"openai_excel_bps_models":                      []string{"gpt-6-astra"},
				"openai_oauth_responses_websockets_v2_enabled": true,
			},
		},
		{
			ID: 91002, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
			Extra: map[string]any{"openai_oauth_responses_websockets_v2_enabled": true},
		},
	}
}

var encryptedMessageCapabilityEngines = []string{"advanced", "advanced_weighted", "legacy_batch", "legacy_lru"}

func encryptedMessageCapabilityService(t *testing.T, engine string, accounts []Account, acquired, released *[]int64) *OpenAIGatewayService {
	t.Helper()
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = engine != "legacy_lru"
	cfg.Gateway.OpenAIWS.LBTopK = 1
	enabled, weighted := "false", "false"
	if engine == "advanced" || engine == "advanced_weighted" {
		enabled = "true"
	}
	if engine == "advanced_weighted" {
		weighted = "true"
	}
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	return &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(enabled, weighted, "false"),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: acquired, releasedIDs: released}),
	}
}

func TestOpenAIEncryptedMessageCapabilities_Pools(t *testing.T) {
	imageBody := strings.Replace(encryptedMessageCapabilityBody, `"input":`, `"tools":[{"type":"image_generation"}],"input":`, 1)
	require.Equal(t, "image_generation", basispoints.NativeFallbackReason([]byte(imageBody)))
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, tc := range []struct {
			name   string
			body   string
			sticky bool
			allBPS bool
			want   int64
		}{
			{"mixed", encryptedMessageCapabilityBody, false, false, 91002},
			{"encrypted_and_image_generation", imageBody, false, false, 91002},
			{"all_bps_and_image_generation", imageBody, false, true, 0},
			{"shorthand", `{"input":[{"role":"user","content":[{"type":"encrypted_content"}]}]}`, false, false, 91002},
			{"all_bps", encryptedMessageCapabilityBody, false, true, 0},
			{"bps_sticky", encryptedMessageCapabilityBody, true, false, 91002},
			{"all_bps_sticky", encryptedMessageCapabilityBody, true, true, 0},
			{"plain_sticky", `{"input":[{"type":"agent_message","content":[{"type":"input_text","text":"test"}]}]}`, true, false, 91001},
			{"reasoning_sticky", `{"input":[{"type":"reasoning","encrypted_content":"synthetic-test-marker"}]}`, true, false, 91001},
		} {
			t.Run(engine+"/"+tc.name, func(t *testing.T) {
				accounts := encryptedMessageCapabilityAccounts()
				if tc.allBPS {
					accounts[1].Extra = accounts[0].Extra
				}
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(tc.body))
				// The retry wrapper may relax only proxy quarantine, never this capability.
				ctx = withOpenAIProxyStreamQuarantineBypass(ctx)
				session := ""
				if tc.sticky {
					session = "encrypted-capability-sticky"
					require.NoError(t, svc.setStickySessionAccountID(ctx, nil, session, accounts[0].ID, time.Hour))
				}
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", session, "gpt-6-astra", nil,
					OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
				if tc.want == 0 {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Nil(t, selection)
					require.Empty(t, acquired, "an incompatible pool must not acquire upstream slots")
					return
				}
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, tc.want, selection.Account.ID)
				if tc.want == 91002 {
					require.NotContains(t, acquired, int64(91001), "filter before TopK and acquisition")
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				require.ElementsMatch(t, acquired, released)
			})
		}
	}
}

func TestOpenAIEncryptedMessageCapabilities_FreshConfiguration(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, sticky := range []bool{false, true} {
			name := engine + "/pool"
			if sticky {
				name = engine + "/sticky"
			}
			t.Run(name, func(t *testing.T) {
				fresh := encryptedMessageCapabilityAccounts()
				stale := fresh[0]
				stale.Extra = nil
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, fresh, &acquired, &released)
				// Keep the backup inside the existing bounded selection window.
				svc.cfg.Gateway.OpenAIWS.LBTopK = 2
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
					snapshotAccounts: []*Account{&stale, &fresh[1]},
					accountsByID:     map[int64]*Account{stale.ID: &stale, fresh[1].ID: &fresh[1]},
				}}
				ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(encryptedMessageCapabilityBody))
				session := ""
				if sticky {
					session = "stale-native-now-bps"
					require.NoError(t, svc.setStickySessionAccountID(ctx, nil, session, stale.ID, time.Hour))
				}
				require.Nil(t, svc.recheckSelectedOpenAIAccountFromDB(ctx, &stale, nil, PlatformOpenAI, "gpt-6-astra", false, OpenAIEndpointCapabilityResponses))
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", session, "gpt-6-astra", nil,
					OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, fresh[1].ID, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				require.ElementsMatch(t, acquired, released, "any stale-candidate slot must be released")
			})
		}
	}
}

func TestOpenAIEncryptedMessageCapabilities_PreviousResponseOwner(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		for _, bpsOwner := range []bool{false, true} {
			name := engine + "/native_owner"
			if bpsOwner {
				name = engine + "/bps_owner"
			}
			t.Run(name, func(t *testing.T) {
				accounts := encryptedMessageCapabilityAccounts()
				if !bpsOwner {
					accounts[0].Extra = accounts[1].Extra
				}
				accounts[0].Priority, accounts[1].Priority = 10, 0
				var acquired, released []int64
				svc := encryptedMessageCapabilityService(t, engine, accounts, &acquired, &released)
				ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(encryptedMessageCapabilityBody))
				if bpsOwner {
					stale := accounts[0]
					stale.Extra = accounts[1].Extra
					svc.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
						snapshotAccounts: []*Account{&stale, &accounts[1]},
						accountsByID:     map[int64]*Account{stale.ID: &stale, accounts[1].ID: &accounts[1]},
					}}
				}
				store := svc.getOpenAIWSStateStore()
				require.NoError(t, store.BindResponseAccount(ctx, 0, "resp_capability_owner", accounts[0].ID, time.Hour))
				selection, decision, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "resp_capability_owner", "", "gpt-6-astra", nil,
					OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
				if bpsOwner {
					require.ErrorIs(t, err, ErrNoAvailableAccounts)
					require.Contains(t, err.Error(), "encrypted_message_unsupported")
					require.Nil(t, selection)
					require.Empty(t, acquired, "must not move a required owner's history to the native backup")
				} else {
					require.NoError(t, err)
					require.NotNil(t, selection)
					require.Equal(t, accounts[0].ID, selection.Account.ID)
					require.True(t, decision.StickyPreviousHit)
					if selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
					}
				}
				owner, err := store.GetResponseAccount(ctx, 0, "resp_capability_owner")
				require.NoError(t, err)
				require.Equal(t, accounts[0].ID, owner, "capability rejection must preserve the owner binding")
			})
		}
	}
}

func TestOpenAIEncryptedMessageCapabilities_GroupBoundary(t *testing.T) {
	for _, engine := range encryptedMessageCapabilityEngines {
		t.Run(engine, func(t *testing.T) {
			accounts := encryptedMessageCapabilityAccounts()
			groupID := int64(91000)
			accounts[0].GroupIDs = []int64{groupID}
			accounts[1].GroupIDs = []int64{groupID + 1}
			svc := encryptedMessageCapabilityService(t, engine, accounts, nil, nil)
			svc.accountRepo = schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}}
			ctx := WithOpenAIResponsesRequestCapabilities(context.Background(), []byte(encryptedMessageCapabilityBody))
			selection, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, &groupID, "", "", "gpt-6-astra", nil,
				OpenAIUpstreamTransportAny, OpenAIEndpointCapabilityResponses, false, false, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection, "native accounts outside the requested group must not become recovery candidates")
		})
	}
}
