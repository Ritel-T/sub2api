//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type nativeCreationExitProber struct{}

func (nativeCreationExitProber) ProbeProxy(ctx context.Context, proxyURL string) (*service.ProxyExitInfo, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	sum := sha256.Sum256([]byte(proxyURL))
	return &service.ProxyExitInfo{IP: fmt.Sprintf("11.12.%d.%d", sum[0], sum[1]), CountryCode: "US", Timezone: "America/Los_Angeles"}, 1, nil
}

func TestNativeAdminCreationCommitsAllDefaultsOrRollsBack(t *testing.T) {
	for _, mode := range []string{"commit", "ready_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			client := testEntClient(t)
			groups := NewAdminGroupRepository(client, integrationDB)
			group := &service.Group{Name: "native-creation-" + mode, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1, SubscriptionType: service.SubscriptionTypeStandard}
			require.NoError(t, groups.Create(ctx, group))
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id=$1", group.ID)
				require.NoError(t, err)
			})
			proxies := NewProxyRepository(client, integrationDB)
			proxy := &service.Proxy{Name: "native-creation-" + mode, Protocol: "http", Host: "native-creation.invalid", Port: 3128, Status: service.StatusActive, FallbackMode: service.FallbackModeNone}
			require.NoError(t, proxies.Create(ctx, proxy))
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", proxy.ID)
				require.NoError(t, err)
			})
			settings := NewSettingRepository(client)
			old, oldErr := settings.GetValue(ctx, service.SettingKeyOAuthAutoConfig)
			t.Cleanup(func() {
				if oldErr == nil {
					require.NoError(t, settings.Set(ctx, service.SettingKeyOAuthAutoConfig, old))
				} else {
					require.NoError(t, settings.Delete(ctx, service.SettingKeyOAuthAutoConfig))
				}
			})
			lf := 20
			cfg := service.DefaultOAuthAutoConfig()
			cfg.Enabled = true
			cfg.Revision = "native-integration"
			cfg.GroupIDs = []int64{group.ID}
			cfg.OpenAIAccountDefaults = &service.OpenAIAccountDefaultsConfig{Enabled: true, DedicatedProxyCountryCodes: []string{"US"}, Templates: map[string]service.OAuthAccountTemplate{
				"business": {Concurrency: 20, LoadFactor: &lf, Priority: 2, ModelMappings: []service.OAuthModelMappingRule{{From: "gpt-6-astra", To: "gpt-6-astra"}, {From: "gpt-6-sol", To: "gpt-6.1-sol"}}},
			}}
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			require.NoError(t, settings.Set(ctx, service.SettingKeyOAuthAutoConfig, string(raw)))
			accounts := NewAdminAccountRepository(client, integrationDB, nil)
			admin := service.NewAdminService(
				nil, nil, groups, accounts, proxies,
				nil, nil, nil, nil, nil,
				nativeCreationExitProber{}, nil, nil,
				client, service.NewSettingService(settings, nil),
				nil, nil, nil, nil, nil, nil, nil, nil,
			)
			if mode == "ready_failure" {
				_, err = integrationDB.ExecContext(ctx, `CREATE FUNCTION native_oauth_creation_ready_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name='native-creation-ready_failure' AND NEW.extra->>'openai_gateway_borrow_initial_ready'='true' THEN RAISE EXCEPTION 'synthetic readiness persistence failure'; END IF; RETURN NEW; END; $$`)
				require.NoError(t, err)
				_, err = integrationDB.ExecContext(ctx, `CREATE TRIGGER native_oauth_creation_ready_failure BEFORE UPDATE OF extra ON accounts FOR EACH ROW EXECUTE FUNCTION native_oauth_creation_ready_failure()`)
				require.NoError(t, err)
				t.Cleanup(func() {
					_, err := integrationDB.ExecContext(ctx, "DROP TRIGGER native_oauth_creation_ready_failure ON accounts")
					require.NoError(t, err)
					_, err = integrationDB.ExecContext(ctx, "DROP FUNCTION native_oauth_creation_ready_failure()")
					require.NoError(t, err)
				})
			}
			var beforeOutbox int64
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox").Scan(&beforeOutbox))
			a, err := admin.CreateAccount(ctx, &service.CreateAccountInput{Name: "native-creation-" + mode, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "chatgpt_account_id": "workspace-owner", "chatgpt_user_id": "user-owner", "plan_type": "self_serve_business_prolite", "model_mapping": map[string]any{"remove-this": "remove-this"}},
				Extra:       map[string]any{"privacy_mode": "enabled", "cost_multiplier": 0.42}, ProxyID: &proxy.ID, Concurrency: 1, Priority: 50, SkipMixedChannelCheck: true})
			if mode == "ready_failure" {
				require.Error(t, err)
				require.Nil(t, a)
				var count, outbox int64
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM accounts WHERE name=$1", "native-creation-"+mode).Scan(&count))
				require.Zero(t, count, "the account row and its bindings must rollback")
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox").Scan(&outbox))
				require.Equal(t, beforeOutbox, outbox)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, a)
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
				require.NoError(t, err)
				_, err = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", a.ID)
				require.NoError(t, err)
			})
			current, err := accounts.GetByID(ctx, a.ID)
			require.NoError(t, err)
			for _, v := range []*service.Account{a, current} {
				require.Equal(t, 20, v.Concurrency)
				require.Equal(t, 20, *v.LoadFactor)
				require.Equal(t, 2, v.Priority)
				require.Equal(t, []int64{group.ID}, v.GroupIDs)
				require.Equal(t, proxy.ID, *v.ProxyID)
				require.Equal(t, map[string]any{"gpt-6-astra": "gpt-6-astra", "gpt-6-sol": "gpt-6.1-sol"}, v.Credentials["model_mapping"])
				require.Equal(t, "whitelist", v.Credentials["model_mapping_mode"])
				require.Equal(t, "fixture-access", v.Credentials["access_token"])
				require.Equal(t, 0.42, v.Extra["cost_multiplier"])
				require.Equal(t, "America/Los_Angeles", v.Extra["openai_request_timezone"])
				require.True(t, service.GatewayBorrowInitialReadyMatchesCurrent(v))
				require.Equal(t, true, v.Extra[service.GatewayBorrowAccountQualityPendingKey])
				require.NotEmpty(t, v.Extra["openai_account_defaults_identity_fingerprint"])
				require.Equal(t, "native-integration", v.Extra["openai_account_defaults_revision"])
			}
		})
	}
}
