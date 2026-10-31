//go:build integration

package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This suite must use an explicitly provisioned, disposable local database.
// It exercises the real repositories and PostgreSQL transactions, not mocks.
func TestGelvPostgresLedgerAndConcurrentRecovery(t *testing.T) {
	dsn := os.Getenv("GELV_TEST_DSN")
	if dsn == "" {
		t.Skip("GELV_TEST_DSN is not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/gelv_test", u.Path)
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(8)
	require.NoError(t, db.PingContext(ctx))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	require.NoError(t, client.Schema.Create(ctx))
	// Exercise the actual production migration as well as the generated Ent index.
	migration, err := os.ReadFile("../../migrations/288_gelv_external_order_identity.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	users := repository.NewUserRepository(client, db)
	codes := repository.NewRedeemCodeRepository(client)
	redeem := service.NewRedeemService(codes, users, nil, nil, nil, client, nil, nil)
	config := service.NewPaymentConfigService(client, nil, []byte(strings.Repeat("t", 32)))
	svc := service.NewPaymentService(client, payment.NewRegistry(), nil, redeem, nil, config, users, nil, nil)
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeGelv).SetName("isolated-test").SetConfig("{}").SetSupportedTypes(payment.TypeGelv).SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, channel, agreement string
		want                     float64
	}{{"wechat_virtual", "wechat_virtual", "", 9.9}, {"apple_iap", "apple_iap", "", 8.8}, {"alipay_zero", "alipay_web", "0", 10}, {"alipay_agreed", "alipay_web", "50", 9.95}} {
		t.Run(tc.name, func(t *testing.T) {
			usr, err := client.User.Create().SetEmail(tc.name + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.invalid").SetPasswordHash("test-only").SetUsername("gelv-local-test").SetStatus("active").Save(ctx)
			require.NoError(t, err)
			reference := "sub2_pg_gelv_" + strconv.FormatInt(usr.ID, 10)
			instanceID := strconv.FormatInt(inst.ID, 10)
			externalID := fmt.Sprintf("%024x", usr.ID)
			quote := map[string]any{"product_id": "3", "pricing_mode": "gelv_channel_cny", "credit_by_channel": map[string]float64{"wechat_virtual": 9.9, "apple_iap": 8.8}, "credited_amount_usd": 8.8, "pricing_basis_currency": "CNY", "base_amount_cny": 8.8, "pay_amount": 10, "currency": "CNY", "checkout_reference": reference}
			snapshot := map[string]any{"gelv_product_id": "3", "schema_version": 2, "provider_key": payment.TypeGelv, "provider_instance_id": instanceID, "merchant_id": "test-partner", "currency": "CNY", "gelv_product_ids": "3,4", "retail_quote": quote, "gelv_android_fee_bps": 100, "gelv_ios_fee_bps": 1200}
			method := payment.TypeGelv
			checkout := "wechat_virtual"
			initial := 8.8
			if tc.channel == "alipay_web" {
				method, checkout, initial = payment.TypeGelvAlipay, "alipay_web", tc.want
				quote["credit_by_channel"] = map[string]float64{"alipay_web": tc.want}
				quote["credited_amount_usd"], quote["base_amount_cny"] = tc.want, tc.want
				snapshot["gelv_alipay_fee_bps"] = tc.agreement
			}
			snapshot["gelv_checkout_channel"] = checkout
			order, err := client.PaymentOrder.Create().SetUserID(usr.ID).SetUserEmail(usr.Email).SetUserName(usr.Username).SetAmount(initial).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("GELV-PG-" + reference).SetOutTradeNo(reference).SetPaymentType(method).SetProviderKey(payment.TypeGelv).SetProviderInstanceID(instanceID).SetProviderSnapshot(snapshot).SetPaymentTradeNo(externalID).SetOrderType(payment.OrderTypeBalance).SetStatus(payment.OrderStatusPending).SetExpiresAt(time.Now().Add(time.Minute)).SetClientIP("127.0.0.1").SetSrcHost("test.example.invalid").Save(ctx)
			require.NoError(t, err)
			n := &payment.PaymentNotification{OrderID: reference, TradeNo: externalID, Amount: 10, Status: payment.NotificationStatusSuccess, Metadata: map[string]string{"partner_id": "test-partner", "partner_order_id": reference, "gelv_order_id": externalID, "product_id": "3", "currency": "CNY", "amount_minor": "1000", "gelv_status": "paid", "wx_order_id": "test-wx-" + externalID, "refunded_minor": "0", "payment_channel": tc.channel, "channel_verified": "true"}}
			n.Metadata["channel_verification_status"] = "verified"
			n.Metadata["fee_rate_bps"] = "100"
			if tc.channel == "apple_iap" {
				n.Metadata["fee_rate_bps"] = "1200"
			}
			n.Metadata["checkout_channel"] = checkout
			n.Metadata["refund_pending"] = "false"
			if tc.channel == "alipay_web" {
				n.Metadata["wx_order_id"] = ""
				n.Metadata["alipay_trade_no"] = "test-alipay-" + externalID
				delete(n.Metadata, "fee_rate_bps")
			}
			// Reject the real balance UPDATE to prove redeem-code use rolls back.
			_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION gelv_test_reject_balance() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id = %d AND NEW.balance > OLD.balance THEN RAISE EXCEPTION 'isolated injected balance failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER gelv_test_reject_balance BEFORE UPDATE OF balance ON users FOR EACH ROW EXECUTE FUNCTION gelv_test_reject_balance();`, usr.ID))
			require.NoError(t, err)
			err = svc.HandlePaymentNotification(ctx, n, payment.TypeGelv)
			require.Error(t, err)
			code, err := codes.GetByCode(ctx, order.RechargeCode)
			require.NoError(t, err)
			require.Equal(t, service.StatusUnused, code.Status)
			savedUser, err := client.User.Get(ctx, usr.ID)
			require.NoError(t, err)
			require.Zero(t, savedUser.Balance)
			_, err = db.ExecContext(ctx, "DROP TRIGGER gelv_test_reject_balance ON users; DROP FUNCTION gelv_test_reject_balance()")
			require.NoError(t, err)
			// Simultaneous confirmations can contend on the fulfillment lease;
			// the final trusted retry must finish once without an extra increment.
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); _ = svc.HandlePaymentNotification(ctx, n, payment.TypeGelv) }()
			}
			wg.Wait()
			require.NoError(t, svc.HandlePaymentNotification(ctx, n, payment.TypeGelv))
			savedUser, err = client.User.Get(ctx, usr.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, savedUser.Balance)
			savedOrder, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, payment.OrderStatusCompleted, savedOrder.Status)
			require.Equal(t, tc.want, savedOrder.Amount)
			if tc.channel == "alipay_web" {
				require.Equal(t, "alipay:"+n.Metadata["alipay_trade_no"], savedOrder.ProviderSnapshot["gelv_wx_order_id"])
				require.Equal(t, n.Metadata["alipay_trade_no"], savedOrder.ProviderSnapshot["gelv_alipay_trade_no"])
				require.NotContains(t, savedOrder.ProviderSnapshot, "gelv_platform_fee_fen")
				require.NotContains(t, savedOrder.ProviderSnapshot, "gelv_fee_rate_bps")
			}
			// Settlement can arrive after credit. Persist exact known evidence,
			// with compare-and-swap on JSON, without changing the credited amount.
			n.Metadata["settlement_state"] = "2"
			n.Metadata["platform_fee_fen"] = "0"
			n.Metadata["cps_fee_fen"] = "0"
			n.Metadata["settled_at"] = "1792176378793"
			require.NoError(t, svc.HandlePaymentNotification(ctx, n, payment.TypeGelv))
			savedOrder, err = client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, "2", savedOrder.ProviderSnapshot["gelv_settlement_state"])
			require.Equal(t, "0", savedOrder.ProviderSnapshot["gelv_platform_fee_fen"])
			savedUser, err = client.User.Get(ctx, usr.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, savedUser.Balance)
			// Simulate a crash after the transactional credit but before COMPLETED.
			_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(payment.OrderStatusRecharging).SetUpdatedAt(time.Now().Add(-10 * time.Minute)).Save(ctx)
			require.NoError(t, err)
			require.NoError(t, svc.HandlePaymentNotification(ctx, n, payment.TypeGelv))
			savedUser, err = client.User.Get(ctx, usr.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, savedUser.Balance)
			code, err = codes.GetByCode(ctx, order.RechargeCode)
			require.NoError(t, err)
			require.Equal(t, service.StatusUsed, code.Status)
			// A gateway bug must not let a second source order claim the same
			// underlying WeChat payment. The production JSON index blocks PAID.
			otherRef := reference + "_other"
			otherQuote := map[string]any{}
			for k, v := range quote {
				otherQuote[k] = v
			}
			otherQuote["checkout_reference"] = otherRef
			otherSnapshot := map[string]any{}
			for k, v := range snapshot {
				otherSnapshot[k] = v
			}
			otherSnapshot["retail_quote"] = otherQuote
			other, err := client.PaymentOrder.Create().SetUserID(usr.ID).SetUserEmail(usr.Email).SetUserName(usr.Username).SetAmount(initial).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("GELV-PG-" + otherRef).SetOutTradeNo(otherRef).SetPaymentType(method).SetProviderKey(payment.TypeGelv).SetProviderInstanceID(instanceID).SetProviderSnapshot(otherSnapshot).SetPaymentTradeNo("other-" + externalID).SetOrderType(payment.OrderTypeBalance).SetStatus(payment.OrderStatusPending).SetExpiresAt(time.Now().Add(time.Minute)).SetClientIP("127.0.0.1").SetSrcHost("test.example.invalid").Save(ctx)
			require.NoError(t, err)
			otherMeta := map[string]string{}
			for k, v := range n.Metadata {
				otherMeta[k] = v
			}
			otherMeta["partner_order_id"] = otherRef
			otherMeta["gelv_order_id"] = other.PaymentTradeNo
			err = svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{OrderID: otherRef, TradeNo: other.PaymentTradeNo, Amount: 10, Status: payment.NotificationStatusSuccess, Metadata: otherMeta}, payment.TypeGelv)
			require.Error(t, err)
			savedUser, err = client.User.Get(ctx, usr.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, savedUser.Balance)
			if tc.channel == "alipay_web" {
				// Race two different source orders sharing one new Alipay receipt.
				// Both get distinct valid upstream order IDs. Only the production
				// JSON unique index can arbitrate the two PAID updates atomically.
				var raceOrders []*dbent.PaymentOrder
				var raceNotes []*payment.PaymentNotification
				for i := 0; i < 2; i++ {
					ref := reference + fmt.Sprintf("_race_%d", i)
					q := map[string]any{}
					for k, v := range quote {
						q[k] = v
					}
					q["checkout_reference"] = ref
					snap := map[string]any{}
					for k, v := range snapshot {
						snap[k] = v
					}
					snap["retail_quote"] = q
					raceOrder, createErr := client.PaymentOrder.Create().SetUserID(usr.ID).SetUserEmail(usr.Email).SetUserName(usr.Username).SetAmount(initial).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("GELV-PG-" + ref).SetOutTradeNo(ref).SetPaymentType(method).SetProviderKey(payment.TypeGelv).SetProviderInstanceID(instanceID).SetProviderSnapshot(snap).SetPaymentTradeNo(fmt.Sprintf("%024x", usr.ID*100+int64(i)+1)).SetOrderType(payment.OrderTypeBalance).SetStatus(payment.OrderStatusPending).SetExpiresAt(time.Now().Add(time.Minute)).SetClientIP("127.0.0.1").SetSrcHost("test.example.invalid").Save(ctx)
					require.NoError(t, createErr)
					m := map[string]string{}
					for k, v := range n.Metadata {
						m[k] = v
					}
					m["partner_order_id"], m["gelv_order_id"], m["alipay_trade_no"] = ref, raceOrder.PaymentTradeNo, "race-alipay-"+externalID
					raceOrders = append(raceOrders, raceOrder)
					raceNotes = append(raceNotes, &payment.PaymentNotification{OrderID: ref, TradeNo: raceOrder.PaymentTradeNo, Amount: 10, Status: payment.NotificationStatusSuccess, Metadata: m})
				}
				start := make(chan struct{})
				results := make(chan error, 2)
				for _, note := range raceNotes {
					go func(note *payment.PaymentNotification) {
						<-start
						results <- svc.HandlePaymentNotification(ctx, note, payment.TypeGelv)
					}(note)
				}
				close(start)
				failures := 0
				for i := 0; i < 2; i++ {
					if <-results != nil {
						failures++
					}
				}
				require.Equal(t, 1, failures, "one source must be rejected by the receipt index")
				completed := 0
				for i, raceOrder := range raceOrders {
					current, readErr := client.PaymentOrder.Get(ctx, raceOrder.ID)
					require.NoError(t, readErr)
					if current.Status == payment.OrderStatusCompleted {
						completed++
					} else {
						require.Equal(t, payment.OrderStatusPending, current.Status)
						require.Error(t, svc.HandlePaymentNotification(ctx, raceNotes[i], payment.TypeGelv))
					}
				}
				require.Equal(t, 1, completed)
				savedUser, err = client.User.Get(ctx, usr.ID)
				require.NoError(t, err)
				require.Equal(t, tc.want*2, savedUser.Balance)
				// A hold observed on a PAID/RECHARGING source prevents the real
				// transaction from consuming its code or updating the balance.
				for _, state := range []string{payment.OrderStatusPaid, payment.OrderStatusRecharging} {
					heldSnapshot := map[string]any{}
					for k, v := range otherSnapshot {
						heldSnapshot[k] = v
					}
					heldSnapshot["gelv_refund_pending"], heldSnapshot["gelv_refunded_minor"] = "true", "0"
					_, err = client.PaymentOrder.UpdateOneID(other.ID).SetStatus(state).SetPaidAt(time.Now()).SetUpdatedAt(time.Now().Add(-10 * time.Minute)).SetProviderSnapshot(heldSnapshot).Save(ctx)
					require.NoError(t, err)
					require.Error(t, svc.RetryFulfillment(ctx, other.ID))
					require.Error(t, svc.ExecuteBalanceFulfillment(ctx, other.ID))
					current, readErr := client.PaymentOrder.Get(ctx, other.ID)
					require.NoError(t, readErr)
					require.Equal(t, state, current.Status)
					require.Zero(t, current.RefundAmount)
					require.Equal(t, "0", current.ProviderSnapshot["gelv_refunded_minor"])
					savedUser, err = client.User.Get(ctx, usr.ID)
					require.NoError(t, err)
					require.Equal(t, tc.want*2, savedUser.Balance)
				}
			}
		})
	}
}
