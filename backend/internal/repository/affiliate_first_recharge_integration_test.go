//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type firstRechargeFixture struct {
	client    *dbent.Client
	users     service.UserRepository
	codes     service.RedeemCodeRepository
	affiliate service.AffiliateRepository
	settings  service.SettingRepository
	redeem    *service.RedeemService
	payment   *service.PaymentService
}

func newFirstRechargeFixture(t *testing.T) *firstRechargeFixture {
	t.Helper()
	f := &firstRechargeFixture{client: testEntClient(t)}
	f.users = NewUserRepository(f.client, integrationDB)
	f.codes = NewRedeemCodeRepository(f.client)
	f.affiliate = NewAffiliateRepository(f.client, integrationDB)
	f.settings = NewSettingRepository(f.client)
	for key, value := range map[string]string{
		service.SettingKeyAffiliateEnabled: "true", service.SettingKeyAffiliateRebateRate: "10",
		service.SettingKeyAffiliateRebateFreezeHours: "0", service.SettingKeyAffiliateRebateDurationDays: "0",
		service.SettingKeyAffiliateRebatePerInviteeCap: "0", service.SettingKeyAffiliateFirstRechargeBonus: "5",
		service.SettingKeyAffiliateFirstRechargeMinAmount: "10",
	} {
		require.NoError(t, f.settings.Set(context.Background(), key, value))
	}
	f.withSettings(f.settings)
	return f
}

func (f *firstRechargeFixture) withSettings(settings service.SettingRepository) {
	aff := service.NewAffiliateService(f.affiliate, service.NewSettingService(settings, &config.Config{}), nil, nil)
	f.redeem = service.NewRedeemService(f.codes, f.users, nil, nil, nil, f.client, nil, aff)
	f.payment = service.NewPaymentService(f.client, nil, nil, f.redeem, nil, nil, f.users, nil, aff)
}

func (f *firstRechargeFixture) user(t *testing.T, label string) *service.User {
	t.Helper()
	return mustCreateCommittedAffiliateWithdrawUser(t, "first-recharge-"+label, 0, 0, 0)
}

func (f *firstRechargeFixture) invited(t *testing.T, inviter *service.User) *service.User {
	t.Helper()
	u := f.user(t, "invitee")
	ok, err := f.affiliate.(service.AffiliateSignupBonusBinder).BindInviterWithSignupBonus(context.Background(), u.ID, inviter.ID, 5)
	require.NoError(t, err)
	require.True(t, ok)
	return u
}

func (f *firstRechargeFixture) code(t *testing.T, amount float64) *service.RedeemCode {
	t.Helper()
	code := &service.RedeemCode{Code: fmt.Sprintf("FIRST-%d", time.Now().UnixNano()), Type: service.RedeemTypeBalance, Value: amount, Status: service.StatusUnused}
	require.NoError(t, f.codes.Create(context.Background(), code))
	return code
}

func (f *firstRechargeFixture) redeemAmount(t *testing.T, userID int64, amount float64) {
	t.Helper()
	_, err := f.redeem.Redeem(context.Background(), userID, f.code(t, amount).Code)
	require.NoError(t, err)
}

func (f *firstRechargeFixture) balance(t *testing.T, userID int64) float64 {
	t.Helper()
	return querySingleFloat(t, context.Background(), f.client, "SELECT balance::double precision FROM users WHERE id=$1", userID)
}

func (f *firstRechargeFixture) order(t *testing.T, userID int64, amount, bonus float64, status string, completed bool) *dbent.PaymentOrder {
	t.Helper()
	now := time.Now()
	code := fmt.Sprintf("FIRST-PAY-%d", now.UnixNano())
	b := f.client.PaymentOrder.Create().SetUserID(userID).SetUserEmail("fixture@example.com").SetUserName("fixture").
		SetAmount(amount).SetBonusAmount(bonus).SetPayAmount(amount - bonus).SetRechargeCode(code).
		SetPaymentType("alipay").SetPaymentTradeNo(code).SetOutTradeNo(code).SetOrderType("balance").
		SetStatus(status).SetPaidAt(now).SetClientIP("127.0.0.1").SetSrcHost("fixture.invalid").SetExpiresAt(now.Add(time.Hour))
	if completed {
		b = b.SetCompletedAt(now)
	}
	o, err := b.Save(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM payment_orders WHERE id=$1", o.ID)
	})
	return o
}

func TestAffiliateFirstRechargePostgres_RechargeGiftAndTenPercentStackOncePerInvitee(t *testing.T) {
	f := newFirstRechargeFixture(t)
	inviter := f.user(t, "inviter")
	invitee := f.invited(t, inviter)
	require.Equal(t, 10.5, f.balance(t, invitee.ID), "signup gift remains distinct")
	code := f.code(t, 10)
	_, err := f.redeem.Redeem(context.Background(), invitee.ID, code.Code)
	require.NoError(t, err)
	_, err = f.redeem.Redeem(context.Background(), invitee.ID, code.Code)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	f.redeemAmount(t, invitee.ID, 20)
	require.Equal(t, 10.5, f.balance(t, inviter.ID))
	require.Equal(t, 40.5, f.balance(t, invitee.ID))
	require.Equal(t, 3.0, querySingleFloat(t, context.Background(), f.client, "SELECT aff_quota::double precision FROM user_affiliates WHERE user_id=$1", inviter.ID))
	require.Zero(t, querySingleFloat(t, context.Background(), f.client, "SELECT total_recharged::double precision FROM users WHERE id=$1", inviter.ID))
	require.Equal(t, 1, querySingleInt(t, context.Background(), f.client, "SELECT count(*) FROM user_affiliate_ledger WHERE user_id=$1 AND action='first_recharge_bonus' AND source_user_id=$2 AND amount=5", inviter.ID, invitee.ID))
	other := f.invited(t, inviter)
	f.redeemAmount(t, other.ID, 10)
	require.Equal(t, 15.5, f.balance(t, inviter.ID), "another invitee earns a separate reward")
}

func TestAffiliateFirstRechargePostgres_IneligibleFirstNeverBecomesLaterEligible(t *testing.T) {
	for _, scenario := range []string{"below_minimum", "disabled", "no_inviter", "historical_code", "historical_completed_order", "credited_refund", "admin_and_signup_only", "uncredited_refund"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFirstRechargeFixture(t)
			inviter := f.user(t, scenario)
			invitee := f.invited(t, inviter)
			ctx := context.Background()
			firstAmount := 10.0
			wantGift := false
			switch scenario {
			case "below_minimum":
				firstAmount = 9.99999999
			case "disabled":
				require.NoError(t, f.settings.Set(ctx, service.SettingKeyAffiliateFirstRechargeBonus, "0"))
			case "no_inviter":
				_, err := f.client.ExecContext(ctx, "UPDATE user_affiliates SET inviter_id=NULL WHERE user_id=$1", invitee.ID)
				require.NoError(t, err)
			case "historical_code":
				old := f.code(t, 5)
				require.NoError(t, f.codes.Use(ctx, old.ID, invitee.ID))
			case "historical_completed_order":
				f.order(t, invitee.ID, 20, 0, service.OrderStatusCompleted, false)
			case "credited_refund":
				f.order(t, invitee.ID, 20, 0, service.OrderStatusRefunded, true)
			case "admin_and_signup_only":
				require.NoError(t, f.users.UpdateBalance(ctx, invitee.ID, 100))
				_, err := f.client.RedeemCode.Create().SetCode(fmt.Sprintf("ADMIN-%d", time.Now().UnixNano())).SetType("admin_balance").SetValue(100).SetStatus("used").SetUsedBy(invitee.ID).SetUsedAt(time.Now()).Save(ctx)
				require.NoError(t, err)
				wantGift = true
			case "uncredited_refund":
				f.order(t, invitee.ID, 20, 0, service.OrderStatusRefunded, false)
				wantGift = true
			}
			f.redeemAmount(t, invitee.ID, firstAmount)
			require.NoError(t, f.settings.Set(ctx, service.SettingKeyAffiliateFirstRechargeBonus, "5"))
			if scenario == "no_inviter" {
				_, err := f.affiliate.BindInviter(ctx, invitee.ID, inviter.ID)
				require.NoError(t, err)
			}
			f.redeemAmount(t, invitee.ID, 10)
			want := 5.5
			if wantGift {
				want += 5
			}
			require.Equal(t, want, f.balance(t, inviter.ID))
			require.Equal(t, 1, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1", invitee.ID))
			if scenario == "below_minimum" {
				require.Equal(t, 1, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1 AND outcome='below_minimum'", invitee.ID))
			}
		})
	}
}

type firstRechargeFailureSettings struct{ service.SettingRepository }

func (r firstRechargeFailureSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("fixture settings read failed")
}

func TestAffiliateFirstRechargePostgres_FailuresRollBackRechargeAndCanRetry(t *testing.T) {
	for _, failure := range []string{"settings_read", "invalid_setting", "ledger_write"} {
		t.Run(failure, func(t *testing.T) {
			f := newFirstRechargeFixture(t)
			inviter := f.user(t, failure)
			invitee := f.invited(t, inviter)
			code := f.code(t, 10)
			ctx := context.Background()
			switch failure {
			case "settings_read":
				f.withSettings(firstRechargeFailureSettings{f.settings})
			case "invalid_setting":
				require.NoError(t, f.settings.Set(ctx, service.SettingKeyAffiliateFirstRechargeBonus, "NaN"))
			case "ledger_write":
				_, err := f.client.ExecContext(ctx, "INSERT INTO user_affiliate_ledger(user_id,action,amount,source_user_id) VALUES($1,'first_recharge_bonus',5,$2)", inviter.ID, invitee.ID)
				require.NoError(t, err)
			}
			_, err := f.redeem.Redeem(ctx, invitee.ID, code.Code)
			require.Error(t, err)
			require.Equal(t, 10.5, f.balance(t, invitee.ID))
			require.Equal(t, 5.5, f.balance(t, inviter.ID))
			stored, err := f.codes.GetByID(ctx, code.ID)
			require.NoError(t, err)
			require.Equal(t, "unused", stored.Status)
			require.Zero(t, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1", invitee.ID))
			_, err = f.client.ExecContext(ctx, "DELETE FROM user_affiliate_ledger WHERE action='first_recharge_bonus' AND source_user_id=$1", invitee.ID)
			require.NoError(t, err)
			require.NoError(t, f.settings.Set(ctx, service.SettingKeyAffiliateFirstRechargeBonus, "5"))
			f.withSettings(f.settings)
			_, err = f.redeem.Redeem(ctx, invitee.ID, code.Code)
			require.NoError(t, err)
			require.Equal(t, 20.5, f.balance(t, invitee.ID))
			require.Equal(t, 10.5, f.balance(t, inviter.ID))
		})
	}
}

func TestAffiliateFirstRechargePostgres_PaymentPrincipalAndConcurrentCodes(t *testing.T) {
	t.Run("payment_bonus_is_excluded_and_retry_is_once", func(t *testing.T) {
		f := newFirstRechargeFixture(t)
		inviter := f.user(t, "principal")
		invitee := f.invited(t, inviter)
		o := f.order(t, invitee.ID, 14.99, 5, service.OrderStatusPaid, false)
		require.NoError(t, f.payment.ExecuteBalanceFulfillment(context.Background(), o.ID))
		require.NoError(t, f.payment.ExecuteBalanceFulfillment(context.Background(), o.ID))
		f.redeemAmount(t, invitee.ID, 10)
		require.Equal(t, 5.5, f.balance(t, inviter.ID))
		require.Equal(t, 1, querySingleInt(t, context.Background(), f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1 AND recharge_amount=9.99 AND outcome='below_minimum' AND source_order_id=$2", invitee.ID, o.ID))
	})
	t.Run("concurrent_payment_and_codes_award_once", func(t *testing.T) {
		f := newFirstRechargeFixture(t)
		inviter := f.user(t, "concurrent")
		invitee := f.invited(t, inviter)
		o := f.order(t, invitee.ID, 15, 5, service.OrderStatusPaid, false)
		const workers = 6
		codes := make([]*service.RedeemCode, workers)
		for i := range codes {
			codes[i] = f.code(t, 10)
		}
		start := make(chan struct{})
		errs := make([]error, workers+1)
		var wg sync.WaitGroup
		for i := range workers + 1 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				if i == workers {
					errs[i] = f.payment.ExecuteBalanceFulfillment(context.Background(), o.ID)
				} else {
					_, errs[i] = f.redeem.Redeem(context.Background(), invitee.ID, codes[i].Code)
				}
			}(i)
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			require.NoError(t, err)
		}
		require.Equal(t, 10.5, f.balance(t, inviter.ID))
		require.Equal(t, 85.5, f.balance(t, invitee.ID))
		require.Equal(t, 7.0, querySingleFloat(t, context.Background(), f.client, "SELECT aff_quota::double precision FROM user_affiliates WHERE user_id=$1", inviter.ID))
		require.Equal(t, 1, querySingleInt(t, context.Background(), f.client, "SELECT count(*) FROM user_affiliate_ledger WHERE action='first_recharge_bonus' AND source_user_id=$1", invitee.ID))
	})
}

func TestAffiliateFirstRechargePostgres_HistoricalMigrationNeverMintsGifts(t *testing.T) {
	f := newFirstRechargeFixture(t)
	ctx := context.Background()
	inviter := f.user(t, "baseline")
	invitee := f.invited(t, inviter)
	old := f.code(t, 20)
	require.NoError(t, f.codes.Use(ctx, old.ID, invitee.ID))
	uncredited := f.invited(t, inviter)
	f.order(t, uncredited.ID, 20, 0, service.OrderStatusRefunded, false)
	adminOnly := f.invited(t, inviter)
	_, err := f.client.RedeemCode.Create().SetCode(fmt.Sprintf("BASE-ADM-%d", time.Now().UnixNano())).SetType("admin_balance").SetValue(100).SetStatus("used").SetUsedBy(adminOnly.ID).Save(ctx)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/287_affiliate_first_recharge.sql")
	require.NoError(t, err)
	_, err = f.client.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.Equal(t, 1, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1 AND outcome='historical' AND reward_amount=0", invitee.ID))
	require.Zero(t, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id IN ($1,$2)", uncredited.ID, adminOnly.ID))
	require.Equal(t, 5.5, f.balance(t, inviter.ID))
	f.redeemAmount(t, invitee.ID, 10)
	require.Equal(t, 5.5, f.balance(t, inviter.ID))
	f.redeemAmount(t, uncredited.ID, 10)
	f.redeemAmount(t, adminOnly.ID, 10)
	require.Equal(t, 15.5, f.balance(t, inviter.ID))
}

func TestAffiliateFirstRechargePostgres_PublicPaymentCodeCannotBypassPrincipalOrOwner(t *testing.T) {
	f := newFirstRechargeFixture(t)
	inviter := f.user(t, "payment-code-owner")
	invitee := f.invited(t, inviter)
	other := f.invited(t, inviter)
	ctx := context.Background()
	for _, status := range []string{service.OrderStatusPaid, service.OrderStatusRefunded} {
		order := f.order(t, invitee.ID, 10, 1, status, false)
		code := &service.RedeemCode{Code: order.RechargeCode, Type: service.RedeemTypeBalance, Value: 10, Status: service.StatusUnused}
		require.NoError(t, f.codes.Create(ctx, code))
		for _, userID := range []int64{invitee.ID, other.ID} {
			before := f.balance(t, userID)
			_, err := f.redeem.Redeem(ctx, userID, code.Code)
			require.Error(t, err)
			require.Equal(t, before, f.balance(t, userID))
			stored, err := f.codes.GetByID(ctx, code.ID)
			require.NoError(t, err)
			require.Equal(t, service.StatusUnused, stored.Status)
		}
		if status == service.OrderStatusPaid {
			require.NoError(t, f.payment.ExecuteBalanceFulfillment(ctx, order.ID))
			require.Equal(t, 1, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1 AND recharge_amount=9 AND outcome='below_minimum'", invitee.ID))
		}
	}
	// A standalone balance code remains a valid qualifying recharge.
	f.redeemAmount(t, other.ID, 10)
	require.Equal(t, 10.5, f.balance(t, inviter.ID))
}

func TestAffiliateFirstRechargePostgres_BonusOnlyPaymentNeverConsumesFirstRecharge(t *testing.T) {
	f := newFirstRechargeFixture(t)
	inviter := f.user(t, "bonus-only")
	invitee := f.invited(t, inviter)
	ctx := context.Background()
	order := f.order(t, invitee.ID, 10, 10, service.OrderStatusPaid, false)
	require.NoError(t, f.payment.ExecuteBalanceFulfillment(ctx, order.ID))
	require.Zero(t, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1", invitee.ID))
	// The migration and runtime fallback must use the same zero-principal rule.
	migration, err := os.ReadFile("../../migrations/287_affiliate_first_recharge.sql")
	require.NoError(t, err)
	_, err = f.client.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	require.Zero(t, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1", invitee.ID))
	f.redeemAmount(t, invitee.ID, 10)
	require.Equal(t, 10.5, f.balance(t, inviter.ID))
}

func TestAffiliateFirstRechargePostgres_InviterAvailabilityAndSingleConnection(t *testing.T) {
	for _, state := range []string{"paused", "deleted", "single_connection"} {
		t.Run(state, func(t *testing.T) {
			f := newFirstRechargeFixture(t)
			inviter := f.user(t, state)
			invitee := f.invited(t, inviter)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			switch state {
			case "paused":
				_, err := f.client.ExecContext(ctx, "UPDATE users SET status='inactive' WHERE id=$1", inviter.ID)
				require.NoError(t, err)
			case "deleted":
				_, err := f.client.ExecContext(ctx, "UPDATE users SET deleted_at=NOW() WHERE id=$1", inviter.ID)
				require.NoError(t, err)
			case "single_connection":
				previousMax := integrationDB.Stats().MaxOpenConnections
				integrationDB.SetMaxOpenConns(1)
				defer integrationDB.SetMaxOpenConns(previousMax)
			}
			_, err := f.redeem.Redeem(ctx, invitee.ID, f.code(t, 10).Code)
			require.NoError(t, err)
			want := 10.5
			if state == "deleted" {
				want = 5.5
			}
			require.Equal(t, want, f.balance(t, inviter.ID))
			require.Equal(t, 20.5, f.balance(t, invitee.ID))
		})
	}
}

func TestAffiliateFirstRechargePostgres_PaymentRewardFailureRetriesWithoutLosingOrDuplicatingGift(t *testing.T) {
	f := newFirstRechargeFixture(t)
	inviter := f.user(t, "payment-retry")
	invitee := f.invited(t, inviter)
	order := f.order(t, invitee.ID, 15, 5, service.OrderStatusPaid, false)
	ctx := context.Background()
	f.withSettings(firstRechargeFailureSettings{f.settings})
	require.Error(t, f.payment.ExecuteBalanceFulfillment(ctx, order.ID))
	require.Equal(t, 10.5, f.balance(t, invitee.ID))
	require.Equal(t, 5.5, f.balance(t, inviter.ID))
	failed, err := f.client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, service.OrderStatusFailed, failed.Status)
	code, err := f.codes.GetByCode(ctx, order.RechargeCode)
	require.NoError(t, err)
	require.Equal(t, service.StatusUnused, code.Status)
	require.Zero(t, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_first_recharges WHERE invitee_user_id=$1", invitee.ID))
	f.withSettings(f.settings)
	require.NoError(t, f.payment.RetryFulfillment(ctx, order.ID))
	require.NoError(t, f.payment.ExecuteBalanceFulfillment(ctx, order.ID))
	require.Equal(t, 25.5, f.balance(t, invitee.ID))
	require.Equal(t, 10.5, f.balance(t, inviter.ID))
	require.Equal(t, 1.0, querySingleFloat(t, ctx, f.client, "SELECT aff_quota::double precision FROM user_affiliates WHERE user_id=$1", inviter.ID))
	require.Equal(t, 1, querySingleInt(t, ctx, f.client, "SELECT count(*) FROM user_affiliate_ledger WHERE action='first_recharge_bonus' AND source_user_id=$1 AND source_order_id=$2", invitee.ID, order.ID))
}
