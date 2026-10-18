//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This is a runtime-to-database contract test: evidence, prices and the billing
// command are all produced by production services. No command or overflow
// evidence is hand-built and no SMTP sender, upstream or periodic job is started.
func TestSubscriptionOverflowRuntimeResolveRecordUsageAndAtomicOutbox(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	fixture := newSubscriptionOverflowIntegrationFixture(t)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	user := mustCreateUser(t, client, &service.User{Email: "runtime-overflow-" + uuid.NewString() + "@example.invalid", PasswordHash: "hash", Balance: 100})
	fixture.userID = user.ID
	dailyLimit := 10.0
	target := mustCreateGroup(t, client, &service.Group{Name: "Runtime PAYG " + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 2})
	fixture.targetGroupID = target.ID
	source := mustCreateGroup(t, client, &service.Group{Name: "Runtime subscription " + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 9, DailyLimitUSD: &dailyLimit})
	fixture.sourceGroupID = source.ID
	_, err := integrationDB.ExecContext(ctx, "UPDATE groups SET subscription_fallback_group_id=$1 WHERE id=$2", target.ID, source.ID)
	require.NoError(t, err)
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: source.ID, DailyUsageUSD: 10, WeeklyUsageUSD: 20, MonthlyUsageUSD: 30})
	now := time.Now().UTC()
	day := timezone.StartOfDay(now)
	_, err = integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET daily_window_start=$1,weekly_window_start=$2,monthly_window_start=$2 WHERE id=$3", day, now.Add(-time.Hour), sub.ID)
	require.NoError(t, err)
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &source.ID, Key: "sk-runtime-overflow-" + uuid.NewString(), Name: "runtime overflow"})
	fixture.keyID, fixture.rawKey = key.ID, key.Key
	account := mustCreateAccount(t, client, &service.Account{Name: "runtime-overflow-account-" + uuid.NewString(), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	fixture.accountID = account.ID
	rateRepo := NewUserGroupRateRepository(integrationDB)
	customRate := 0.42
	require.NoError(t, rateRepo.SyncUserGroupRates(ctx, user.ID, map[int64]*float64{target.ID: &customRate}))
	keyRepo := NewAPIKeyRepository(client, integrationDB)
	groupRepo := NewGroupRepository(client, integrationDB)
	userRepo := NewUserRepository(client, integrationDB)
	subRepo := NewUserSubscriptionRepository(client)
	auth := service.NewAPIKeyService(keyRepo, userRepo, groupRepo, subRepo, rateRepo, nil, cfg)
	subscriptions := service.NewSubscriptionService(groupRepo, subRepo, nil, client, cfg)
	t.Cleanup(subscriptions.Stop)
	freshKey, err := auth.GetByKeyUncached(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, target.ID, *freshKey.Group.SubscriptionFallbackGroupID)
	effective, activeSubscription, err := auth.ResolveSubscriptionBilling(ctx, freshKey, subscriptions)
	require.NoError(t, err)
	require.Nil(t, activeSubscription)
	require.Equal(t, target.ID, *effective.GroupID)
	require.NotNil(t, effective.SubscriptionOverflow)
	require.Equal(t, "daily", effective.SubscriptionOverflow.Dimension)
	require.Equal(t, 10.0, effective.SubscriptionOverflow.DailyUsageUSD)
	require.Equal(t, source.ID, *freshKey.GroupID)
	// Static fallback pricing keeps the entire test offline. The public runtime
	// still selects the target user's rate and calculates the charge itself.
	billing := service.NewBillingService(cfg, nil)
	ledger := NewUsageBillingRepository(client, integrationDB)
	usage := NewUsageLogRepository(client, integrationDB)
	deferred := service.NewDeferredService(nil, nil, time.Hour)
	gateway := service.NewGatewayService(
		nil, groupRepo, usage, ledger, userRepo, subRepo, rateRepo,
		nil, cfg, nil, nil, billing, nil, nil, nil, nil, deferred,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	requestID := "runtime-overflow-" + uuid.NewString()
	record := func() error {
		return gateway.RecordUsage(ctx, &service.RecordUsageInput{
			Result: &service.ForwardResult{RequestID: requestID, Model: "gpt-6-astra", Usage: service.ClaudeUsage{InputTokens: 1000, OutputTokens: 500}},
			APIKey: effective, User: effective.User, Account: account, Subscription: activeSubscription, APIKeyService: auth,
		})
	}
	require.NoError(t, record())
	require.NoError(t, record()) // The same real request is replayed, not billed twice.
	// The real CreateBestEffort batch path waits on each request's resultCh.
	// This uncancelled context cannot take its early cancellation branch, so
	// both writes finish before RecordUsage returns and fixture cleanup runs.
	// Built-in gpt-6-astra standard pricing: .01 input + .025 output.
	const expectedBase = 0.035
	const expectedCharge = expectedBase * 0.42
	var balance, daily, weekly, monthly float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
	require.InDelta(t, 100-expectedCharge, balance, 1e-8)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=$1", sub.ID).Scan(&daily, &weekly, &monthly))
	require.Equal(t, []float64{10, 20, 30}, []float64{daily, weekly, monthly})
	var ledgerCount, outboxCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", requestID, key.ID).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM subscription_overflow_email_outbox WHERE user_id=$1", user.ID).Scan(&outboxCount))
	require.Equal(t, 1, outboxCount)
	var raw []byte
	var state, balanceTX, ledgerTX, outboxTX string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT o.payload,o.status,u.xmin::text,d.xmin::text,o.xmin::text FROM subscription_overflow_email_outbox o JOIN users u ON u.id=o.user_id JOIN usage_billing_dedup d ON d.request_id=$2 AND d.api_key_id=$3 WHERE o.user_id=$1`, user.ID, requestID, key.ID).Scan(&raw, &state, &balanceTX, &ledgerTX, &outboxTX))
	require.Equal(t, balanceTX, ledgerTX)
	require.Equal(t, balanceTX, outboxTX, "balance, billing ledger and outbox must originate in the same PostgreSQL transaction")
	require.Equal(t, "pending", state)
	var payload service.SubscriptionOverflowNotificationPayload
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, source.ID, payload.SourceGroupID)
	require.Equal(t, target.ID, payload.TargetGroupID)
	require.InDelta(t, customRate, payload.RateMultiplier, 1e-10)
	require.InDelta(t, expectedCharge, payload.BalanceCost, 1e-8)
	require.InDelta(t, balance, payload.BalanceAfter, 1e-8)
	var actual, total, rate float64
	var loggedGroup int64
	var loggedSubscription sql.NullInt64
	// No polling is needed: the writer has acknowledged both record calls.
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT actual_cost,total_cost,rate_multiplier,group_id,subscription_id FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", requestID, key.ID).Scan(&actual, &total, &rate, &loggedGroup, &loggedSubscription))
	require.Equal(t, target.ID, loggedGroup)
	require.False(t, loggedSubscription.Valid)
	require.InDelta(t, expectedBase, total, 1e-8)
	require.InDelta(t, expectedCharge, actual, 1e-8)
	require.InDelta(t, customRate, rate, 1e-10)
	// A fresh DB read must notice quota recovery on the next request, with the
	// persisted API key still bound to its original subscription group.
	_, err = integrationDB.ExecContext(ctx, "UPDATE user_subscriptions SET daily_usage_usd=0 WHERE id=$1", sub.ID)
	require.NoError(t, err)
	restored, restoredSub, err := auth.ResolveSubscriptionBilling(ctx, effective, subscriptions)
	require.NoError(t, err)
	require.NotNil(t, restoredSub)
	require.Equal(t, source.ID, *restored.GroupID)
	require.Nil(t, restored.SubscriptionOverflow)
}
