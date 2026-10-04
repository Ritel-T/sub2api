//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type balancePurchaseSettings struct {
	service.SettingRepository
	values map[string]string
}

func (r *balancePurchaseSettings) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}
func (r *balancePurchaseSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

// Own a new disposable PostgreSQL container; never read any production DSN.
func TestBalanceSubscriptionPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	pg, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23", tcpostgres.WithDatabase("balance_subscription_test"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("test-only-password"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		require.NoError(t, pg.Terminate(cleanupCtx))
	})
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(12)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NoError(t, client.Schema.Create(ctx))
	settings := &balancePurchaseSettings{values: map[string]string{
		service.SettingPaymentEnabled: "true", service.SettingBalanceRetailPricingEnabled: "true",
		service.SettingKeySubscriptionEnabled: "true",
		service.SettingRechargeBonusMode:      service.RechargeBonusModeDiscount,
		service.SettingRechargeBonusTiers:     `[{"min_amount":0,"bonus_percent":25}]`,
	}}
	cfg := service.NewPaymentConfigService(client, settings, nil)
	groups := repository.NewGroupRepository(client, db)
	subs := service.NewSubscriptionService(groups, repository.NewUserSubscriptionRepository(client), nil, client, nil)
	t.Cleanup(subs.Stop)
	svc := service.NewPaymentService(client, nil, nil, nil, subs, cfg, nil, groups, nil)
	g, err := client.Group.Create().SetName("fixture-subscription").SetPlatform("openai").SetSubscriptionType("subscription").SetStatus("active").SetWeeklyLimitUsd(150).Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetName("fixture-Plus").SetGroupID(g.ID).SetPrice(138).
		SetCurrency("CNY").SetValidityDays(30).SetValidityUnit("day").SetFeatures("").SetDescription("").SetProductName("").SetForSale(true).Save(ctx)
	require.NoError(t, err)
	sequence := 0
	makeUser := func(balance float64) *dbent.User {
		sequence++
		u, err := client.User.Create().SetEmail(fmt.Sprintf("balance-fixture-%d@example.test", sequence)).SetPasswordHash("fixture-not-real").SetUsername("fixture").SetBalance(balance).SetTotalRecharged(1000).Save(ctx)
		require.NoError(t, err)
		return u
	}
	request := func(userID int64, nonce string) service.BalanceSubscriptionRequest {
		return service.BalanceSubscriptionRequest{PlanID: plan.ID, PurchaseNonce: nonce, ExpectedPrice: 138,
			ExpectedUserID: userID, ExpectedCurrency: "CNY", ExpectedGroupID: g.ID, ExpectedDays: 30, ExpectedUnit: "day"}
	}
	assertBalance := func(uid int64, expected float64) {
		u, err := client.User.Get(ctx, uid)
		require.NoError(t, err)
		require.Equal(t, expected, u.Balance)
		require.Equal(t, 1000.0, u.TotalRecharged, "spending must not become another recharge")
	}
	t.Run("concurrent_double_click_debits_and_assigns_once", func(t *testing.T) {
		u := makeUser(500)
		req := request(u.ID, "double-click-fixture-000000001")
		start := make(chan struct{})
		results := make(chan *service.BalanceSubscriptionResult, 2)
		errors := make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				result, err := svc.BuySubscriptionWithBalance(ctx, u.ID, req)
				results <- result
				errors <- err
			}()
		}
		close(start)
		for range 2 {
			require.NoError(t, <-errors)
		}
		first, second := <-results, <-results
		require.Equal(t, first.OrderID, second.OrderID)
		assertBalance(u.ID, 362)
		sub, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(u.ID)).Only(ctx)
		require.NoError(t, err)
		require.InDelta(t, 30, sub.ExpiresAt.Sub(sub.StartsAt).Hours()/24, .01)
		o, err := client.PaymentOrder.Get(ctx, first.OrderID)
		require.NoError(t, err)
		require.Equal(t, "COMPLETED", o.Status)
		require.Equal(t, 0.0, o.PayAmount, "site credit spending must not count as new cash revenue")
		require.Zero(t, o.BonusAmount, "gateway recharge promotions cannot alter site-credit subscription purchases")
		require.Empty(t, o.RechargeCode)
		require.Equal(t, "internal_balance", *o.ProviderKey)
		count, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(u.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		count, err = client.PaymentAuditLog.Query().Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 3, count)
		count, err = client.RedeemCode.Query().Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		count, err = client.PaymentExternalOrder.Query().Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		replay, err := svc.BuySubscriptionWithBalance(ctx, u.ID, req)
		require.NoError(t, err)
		require.Equal(t, first.OrderID, replay.OrderID)
		other := makeUser(500)
		req.ExpectedUserID = other.ID
		_, err = svc.BuySubscriptionWithBalance(ctx, other.ID, req)
		require.Equal(t, "PURCHASE_NONCE_CONFLICT", infraerrors.Reason(err))
		assertBalance(other.ID, 500)
	})
	t.Run("renewal_and_frozen_replay_after_plan_changed", func(t *testing.T) {
		u := makeUser(500)
		_, err := svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "renewal-fixture-000000000001"))
		require.NoError(t, err)
		before, err := client.UserSubscription.Query().Where(usersubscription.UserIDEQ(u.ID)).Only(ctx)
		require.NoError(t, err)
		req := request(u.ID, "renewal-fixture-000000000002")
		result, err := svc.BuySubscriptionWithBalance(ctx, u.ID, req)
		require.NoError(t, err)
		require.True(t, result.Renewed)
		after, err := client.UserSubscription.Get(ctx, before.ID)
		require.NoError(t, err)
		require.InDelta(t, 30, after.ExpiresAt.Sub(before.ExpiresAt).Hours()/24, .01)
		assertBalance(u.ID, 224)
		require.NoError(t, client.SubscriptionPlan.UpdateOneID(plan.ID).SetPrice(678).SetValidityDays(60).SetForSale(false).Exec(ctx))
		replay, err := svc.BuySubscriptionWithBalance(ctx, u.ID, req)
		require.NoError(t, err)
		require.Equal(t, result.OrderID, replay.OrderID)
		assertBalance(u.ID, 224)
		require.NoError(t, client.SubscriptionPlan.UpdateOneID(plan.ID).SetPrice(138).SetValidityDays(30).SetForSale(true).Exec(ctx))
	})
	t.Run("insufficient_and_disabled_and_quote_changes_do_not_debit", func(t *testing.T) {
		u := makeUser(137)
		_, err := svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "insufficient-fixture-000001"))
		require.Equal(t, "INSUFFICIENT_BALANCE", infraerrors.Reason(err))
		assertBalance(u.ID, 137)
		require.NoError(t, client.User.UpdateOneID(u.ID).SetBalance(500).Exec(ctx))
		settings.values[service.SettingPaymentEnabled] = "false"
		_, err = svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "disabled-fixture-0000000001"))
		require.Equal(t, "PAYMENT_DISABLED", infraerrors.Reason(err))
		settings.values[service.SettingPaymentEnabled] = "true"
		for _, change := range []func() error{
			func() error { return client.SubscriptionPlan.UpdateOneID(plan.ID).SetPrice(139).Exec(ctx) },
			func() error {
				return client.SubscriptionPlan.UpdateOneID(plan.ID).SetPrice(138).SetValidityDays(29).Exec(ctx)
			},
			func() error {
				return client.SubscriptionPlan.UpdateOneID(plan.ID).SetValidityDays(30).SetCurrency("USD").Exec(ctx)
			},
		} {
			require.NoError(t, change())
			_, err = svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "changed-quote-fixture-00001"))
			require.Equal(t, "PLAN_PRICE_CHANGED", infraerrors.Reason(err))
			assertBalance(u.ID, 500)
		}
		require.NoError(t, client.SubscriptionPlan.UpdateOneID(plan.ID).SetCurrency("CNY").Exec(ctx))
	})
	t.Run("subscription_failure_rolls_back_debit_order_and_audits", func(t *testing.T) {
		u := makeUser(500)
		_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE FUNCTION fixture_fail_sub() RETURNS trigger AS $$ BEGIN IF NEW.user_id = %d THEN RAISE EXCEPTION 'fixture assignment failure'; END IF; RETURN NEW; END; $$ LANGUAGE plpgsql; CREATE TRIGGER fixture_fail_sub BEFORE INSERT ON user_subscriptions FOR EACH ROW EXECUTE FUNCTION fixture_fail_sub();`, u.ID))
		require.NoError(t, err)
		_, err = svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "rollback-fixture-0000000001"))
		require.Error(t, err)
		assertBalance(u.ID, 500)
		count, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(u.ID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		count, err = client.UserSubscription.Query().Where(usersubscription.UserIDEQ(u.ID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		_, err = db.ExecContext(ctx, "DROP TRIGGER fixture_fail_sub ON user_subscriptions; DROP FUNCTION fixture_fail_sub();")
		require.NoError(t, err)
		_, err = svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "rollback-fixture-0000000001"))
		require.NoError(t, err)
		assertBalance(u.ID, 362)
	})
	t.Run("one_connection_real_repositories_do_not_wait_for_another_connection", func(t *testing.T) {
		u := makeUser(500)
		for key, value := range map[string]string{service.SettingPaymentEnabled: "true", service.SettingBalanceRetailPricingEnabled: "true", service.SettingKeySubscriptionEnabled: "true"} {
			require.NoError(t, client.Setting.Create().SetKey(key).SetValue(value).Exec(ctx))
		}
		realCfg := service.NewPaymentConfigService(client, repository.NewSettingRepository(client), nil)
		realSvc := service.NewPaymentService(client, nil, nil, nil, subs, realCfg, nil, groups, nil)
		db.SetMaxOpenConns(1)
		defer db.SetMaxOpenConns(12)
		boundedCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		result, err := realSvc.BuySubscriptionWithBalance(boundedCtx, u.ID, request(u.ID, "single-connection-fixture-0001"))
		require.NoError(t, err)
		assertBalance(u.ID, 362)
		_, _, err = realSvc.PrepareRefund(ctx, result.OrderID, 138, "fixture", false, false)
		require.Equal(t, "BALANCE_SUBSCRIPTION_REFUND_MANUAL", infraerrors.Reason(err))
	})
	t.Run("cannot_charge_a_different_expected_user_or_an_unextendable_term", func(t *testing.T) {
		u := makeUser(500)
		req := request(u.ID+1, "identity-change-fixture-00001")
		_, err := svc.BuySubscriptionWithBalance(ctx, u.ID, req)
		require.Equal(t, "INVALID_INPUT", infraerrors.Reason(err))
		assertBalance(u.ID, 500)
		_, err = client.UserSubscription.Create().SetUserID(u.ID).SetGroupID(g.ID).SetStartsAt(time.Now()).SetExpiresAt(service.MaxExpiresAt).Save(ctx)
		require.NoError(t, err)
		_, err = svc.BuySubscriptionWithBalance(ctx, u.ID, request(u.ID, "unextendable-term-fixture-0001"))
		require.Equal(t, "PLAN_VALIDITY_INVALID", infraerrors.Reason(err))
		assertBalance(u.ID, 500)
		count, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(u.ID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}
