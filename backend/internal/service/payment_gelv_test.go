//go:build unit

package service

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func gelvServiceOrder(t *testing.T, status string) (*PaymentService, *dbent.PaymentOrder, *mockUserRepo, *paymentFulfillmentRedeemRepo) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	u, err := client.User.Create().SetEmail("gelv-test@example.com").SetPasswordHash("hash").SetUsername("gelv-test").Save(ctx)
	require.NoError(t, err)
	q := &RetailQuote{ProductID: "3", PricingMode: "gelv_channel_cny", CreditByChannel: map[string]float64{"wechat_virtual": 9.9, "apple_iap": 8.8}, CreditedAmountUSD: 8.8, PricingBasisCurrency: "CNY", BaseAmountCNY: 8.8, PayAmount: 10, Currency: "CNY", CheckoutReference: "sub2_gelv_service_test"}
	sel := &payment.InstanceSelection{InstanceID: "1", ProviderKey: payment.TypeGelv, PaymentMode: "qrcode", Config: map[string]string{"partnerId": "partner-test", "productIds": "3,4"}}
	snap := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{RetailQuote: q})
	o, err := client.PaymentOrder.Create().SetUserID(u.ID).SetUserEmail(u.Email).SetUserName(u.Username).SetAmount(8.8).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("GELV-TEST-RECHARGE").SetOutTradeNo(q.CheckoutReference).SetPaymentType(payment.TypeGelv).SetPaymentTradeNo("0123456789abcdef01234567").SetProviderKey(payment.TypeGelv).SetProviderInstanceID("1").SetProviderSnapshot(snap).SetOrderType(payment.OrderTypeBalance).SetStatus(status).SetExpiresAt(time.Now().Add(-time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("site.example.com").Save(ctx)
	require.NoError(t, err)
	users := &mockUserRepo{getByIDUser: &User{ID: u.ID, Email: u.Email, Username: u.Username, Status: payment.EntityStatusActive}}
	users.updateBalanceFn = func(_ context.Context, _ int64, amount float64) error {
		users.getByIDUser.Balance += amount
		return nil
	}
	codes := &paymentFulfillmentRedeemRepo{}
	redeem := NewRedeemService(codes, users, nil, nil, nil, client, nil, nil)
	svc := &PaymentService{entClient: client, userRepo: users, redeemService: redeem, registry: payment.NewRegistry()}
	return svc, o, users, codes
}

func gelvServiceMetadata(o *dbent.PaymentOrder, channel string) map[string]string {
	rate := "100"
	if channel == "apple_iap" {
		rate = "1200"
	}
	return map[string]string{"partner_id": "partner-test", "partner_order_id": o.OutTradeNo, "gelv_order_id": o.PaymentTradeNo, "product_id": "3", "currency": "CNY", "amount_minor": "1000", "gelv_status": "paid", "wx_order_id": "wx-paid-test", "refunded_minor": "0", "payment_channel": channel, "channel_verified": "true", "channel_verification_status": "verified", "fee_rate_bps": rate}
}

func TestGelvActualChannelResolvesFrozenCreditOnceAfterExpiry(t *testing.T) {
	for _, tc := range []struct {
		channel string
		want    float64
	}{{"wechat_virtual", 9.9}, {"apple_iap", 8.8}} {
		t.Run(tc.channel, func(t *testing.T) {
			s, o, users, codes := gelvServiceOrder(t, OrderStatusExpired)
			ctx := context.Background()
			m := gelvServiceMetadata(o, tc.channel)
			for i := 0; i < 3; i++ {
				require.NoError(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
			}
			saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusCompleted, saved.Status)
			require.Equal(t, tc.want, saved.Amount)
			require.Equal(t, tc.want, users.getByIDUser.Balance)
			require.Len(t, codes.useCalls, 1)
			require.Equal(t, 1, codes.createCalls)
			require.Equal(t, tc.channel, saved.ProviderSnapshot["gelv_payment_channel"])
			// Keep both originally accepted outcomes unchanged for order replay/audit.
			require.Equal(t, 8.8, PaymentOrderRetailQuote(saved).CreditedAmountUSD)
		})
	}
}

func TestGelvUnverifiedOrForeignPaymentCannotCredit(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"payment_channel", "android"}, {"payment_channel", ""}, {"channel_verified", "false"}, {"channel_verification_status", "conflict"}, {"fee_rate_bps", "1200"}, {"fee_rate_bps", ""}, {"partner_order_id", "other_order_123"}, {"partner_id", "other-partner"}, {"product_id", "999"}, {"amount_minor", "999"}, {"refunded_minor", "100"}, {"wx_order_id", ""}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
			m := gelvServiceMetadata(o, "wechat_virtual")
			m[tc.key] = tc.value
			err := s.confirmPayment(context.Background(), o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m)
			require.Error(t, err)
			require.Zero(t, users.getByIDUser.Balance)
			require.Zero(t, codes.createCalls)
			saved, err := s.entClient.PaymentOrder.Get(context.Background(), o.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusPending, saved.Status)
		})
	}
}

func TestGelvResumeAfterCreditBeforeCompletedDoesNotCreditAgain(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPaid)
	ctx := context.Background()
	m := gelvServiceMetadata(o, "wechat_virtual")
	o.ProviderSnapshot["gelv_payment_channel"] = "wechat_virtual"
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetAmount(9.9).SetProviderSnapshot(o.ProviderSnapshot).Save(ctx)
	require.NoError(t, err)
	uid := o.UserID
	users.getByIDUser.Balance = 9.9
	codes.codesByCode = map[string]*RedeemCode{o.RechargeCode: {ID: 11, Code: o.RechargeCode, Type: RedeemTypeBalance, Value: 9.9, Status: StatusUsed, UsedBy: &uid}}
	require.NoError(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Empty(t, codes.useCalls)
	saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	m["payment_channel"] = "apple_iap"
	require.Error(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
}

type gelvRecoveryProvider struct {
	paymentOrderLifecycleQueryProvider
	createCalls int
	createdRef  string
}

func (p *gelvRecoveryProvider) CreatePayment(_ context.Context, r payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	p.createCalls++
	p.createdRef = r.OrderID
	return &payment.CreatePaymentResponse{TradeNo: "0123456789abcdef01234567"}, nil
}

func TestGelvUnknownCreateRestoresSameOrderAndRetainsImage(t *testing.T) {
	s, o, users, _ := gelvServiceOrder(t, OrderStatusPending)
	ctx := context.Background()
	oldID := o.PaymentTradeNo
	_, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetExpiresAt(time.Now().Add(time.Minute)).SetPaymentTradeNo("").SetQrCode("stored-mini-program-image").Save(ctx)
	require.NoError(t, err)
	o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	m := gelvServiceMetadata(o, "wechat_virtual")
	m["gelv_order_id"] = oldID
	p := &gelvRecoveryProvider{paymentOrderLifecycleQueryProvider: paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: oldID, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: m}}}
	recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, p)
	require.NoError(t, err)
	require.True(t, recovered)
	require.Equal(t, o.OutTradeNo, p.createdRef)
	require.Equal(t, 1, p.createCalls)
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, "stored-mini-program-image", *saved.QrCode)
	count, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestGelvExternalOrderUniqueAndRefundReviewDoesNotMutateCredit(t *testing.T) {
	s, o, users, _ := gelvServiceOrder(t, OrderStatusCompleted)
	ctx := context.Background()
	users.getByIDUser.Balance = 9.9
	other := s.entClient.PaymentOrder.Create().SetUserID(o.UserID).SetUserEmail(o.UserEmail).SetUserName(o.UserName).SetAmount(8.8).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("OTHER").SetOutTradeNo("sub2_other_source_order").SetPaymentType(payment.TypeGelv).SetProviderKey(payment.TypeGelv).SetProviderInstanceID("1").SetPaymentTradeNo(o.PaymentTradeNo).SetOrderType(payment.OrderTypeBalance).SetStatus(OrderStatusPending).SetExpiresAt(time.Now()).SetClientIP("127.0.0.1").SetSrcHost("site.example.com")
	_, err := other.Save(ctx)
	require.Error(t, err)
	m := gelvServiceMetadata(o, "wechat_virtual")
	m["gelv_status"] = "partially_refunded"
	m["refunded_minor"] = "100"
	p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusRefunded, Amount: 10, Metadata: m}}
	for i := 0; i < 2; i++ {
		recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, p)
		require.NoError(t, err)
		require.False(t, recovered)
	}
	count, err := s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionEQ("GELV_REFUND_REVIEW_REQUIRED")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	m["gelv_status"] = "refunded"
	m["refunded_minor"] = "1000"
	m["channel_verification_status"] = "conflict"
	m["channel_verified"] = "false"
	_, err = s.reconcileGelvOrderWithProvider(ctx, o, p)
	require.NoError(t, err)
	count, err = s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionEQ("GELV_REFUND_REVIEW_REQUIRED")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	conflicts, err := s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionEQ("GELV_CHANNEL_REVIEW_REQUIRED")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, conflicts)
}

func TestGelvProviderConfigSecretsAndCapabilityGuards(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	s := NewPaymentConfigService(client, nil, []byte(strings.Repeat("k", 32)))
	config := map[string]string{"partnerId": "partner-test", "partnerSecret": strings.Repeat("fake-secret-", 4), "productIds": "3,4,5,6,7,8"}
	first, err := s.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeGelv, Name: "mini-program", Config: config, SupportedTypes: []string{payment.TypeGelv}, Enabled: true, PaymentMode: "qrcode"})
	require.NoError(t, err)
	require.NotContains(t, first.Config, config["partnerSecret"])
	require.True(t, isSensitiveProviderConfigField(payment.TypeGelv, "PartnerSecret"))
	_, err = s.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeGelv, Name: "second", Config: config, SupportedTypes: []string{payment.TypeGelv}, Enabled: true})
	require.Error(t, err)
	refunded := true
	_, err = s.UpdateProviderInstance(ctx, first.ID, UpdateProviderInstanceRequest{RefundEnabled: &refunded})
	require.Error(t, err)
}

func TestGelvQuoteFreezesBothChannelCreditsAndCashLimits(t *testing.T) {
	s, _, _, _ := gelvServiceOrder(t, OrderStatusPending)
	ctx := context.Background()
	key := []byte(strings.Repeat("q", 32))
	configSvc := NewPaymentConfigService(s.entClient, nil, key)
	_, err := configSvc.CreateProviderInstance(ctx, CreateProviderInstanceRequest{ProviderKey: payment.TypeGelv, Name: "Shared mini-program products", Config: map[string]string{"partnerId": "partner-test", "partnerSecret": strings.Repeat("fake-test-secret-", 3), "productIds": "3,4,5,6,7,8"}, SupportedTypes: []string{payment.TypeGelv}, Enabled: true, PaymentMode: "qrcode"})
	require.NoError(t, err)
	s.configService = configSvc
	s.loadBalancer = payment.NewDefaultLoadBalancer(s.entClient, key)
	s.resumeService = NewPaymentResumeService(key)
	cfg := &PaymentConfig{MinAmount: 10, MaxAmount: 500, BalanceRetailPricingEnabled: true, BalanceRetailQuoteTTLSeconds: 900}
	response, err := s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelv, OrderType: payment.OrderTypeBalance, Amount: 8.8}, cfg)
	require.NoError(t, err)
	q := response.RetailQuote
	require.Equal(t, 10.0, q.PayAmount)
	require.Equal(t, 9.9, q.CreditByChannel["wechat_virtual"])
	require.Equal(t, 8.8, q.CreditByChannel["apple_iap"])
	require.Empty(t, q.FX)
	require.Zero(t, q.TotalAmountGBP)
	claims, err := s.parseRetailQuote(response.QuoteToken)
	require.NoError(t, err)
	require.Equal(t, q.CreditByChannel, claims.Quote.CreditByChannel)
	cfg.DailyLimit = 9
	cfg.MaxPendingOrders = 3
	_, err = s.createRetailQuotedOrder(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelv, OrderType: payment.OrderTypeBalance, Amount: 8.8, QuoteToken: response.QuoteToken}, cfg, s.userRepo.(*mockUserRepo).getByIDUser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "daily USD product limit exceeded")
	_, err = s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelv, Amount: 9.9}, cfg)
	require.Error(t, err)
	cfg.MinAmount = 30
	_, err = s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelv, Amount: 8.8}, cfg)
	require.Error(t, err)
}

func TestGelvSettlementOnlyUpdatesEvidenceAndNeverRecredits(t *testing.T) {
	s, o, users, codes := gelvServiceOrder(t, OrderStatusPending)
	ctx := context.Background()
	m := gelvServiceMetadata(o, "wechat_virtual")
	m["settlement_state"] = "3"
	m["channel_verified_at"] = "1792089978793"
	require.NoError(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 10, payment.TypeGelv, m))
	o, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, "100", o.ProviderSnapshot["gelv_fee_rate_bps"])
	_, exists := o.ProviderSnapshot["gelv_platform_fee_fen"]
	require.False(t, exists)
	m["settlement_state"] = "2"
	m["platform_fee_fen"] = "10"
	m["cps_fee_fen"] = "0"
	m["settled_at"] = "1792176378793"
	p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 10, Metadata: m}}
	for i := 0; i < 2; i++ {
		recovered, pollErr := s.reconcileGelvOrderWithProvider(ctx, o, p)
		require.NoError(t, pollErr)
		require.False(t, recovered)
		o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
		require.NoError(t, err)
	}
	require.Equal(t, "10", o.ProviderSnapshot["gelv_platform_fee_fen"])
	require.Equal(t, "0", o.ProviderSnapshot["gelv_cps_fee_fen"])
	require.Equal(t, 9.9, users.getByIDUser.Balance)
	require.Len(t, codes.useCalls, 1)
	delete(m, "platform_fee_fen")
	delete(m, "cps_fee_fen")
	m["settlement_state"] = "3"
	_, err = s.reconcileGelvOrderWithProvider(ctx, o, p)
	require.NoError(t, err)
	o, err = s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, "2", o.ProviderSnapshot["gelv_settlement_state"])
	require.Equal(t, "10", o.ProviderSnapshot["gelv_platform_fee_fen"])
}
