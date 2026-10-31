//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func newArbitraryAlipayQuoteService(t *testing.T) (*PaymentService, *PaymentConfig) {
	t.Helper()
	s, _, _, _ := gelvServiceOrder(t, OrderStatusPending)
	key := []byte(strings.Repeat("q", 32))
	s.configService = NewPaymentConfigService(s.entClient, nil, key)
	_, err := s.configService.CreateProviderInstance(context.Background(), CreateProviderInstanceRequest{
		ProviderKey: payment.TypeGelv, Name: "shared", Config: map[string]string{"partnerId": "partner-test", "partnerSecret": strings.Repeat("fake-test-secret-", 3), "productIds": "3,4,5,6,7,8", "alipayFeeRateBps": "60"}, SupportedTypes: []string{payment.TypeGelv, payment.TypeGelvAlipay}, Enabled: true, PaymentMode: "qrcode",
	})
	require.NoError(t, err)
	s.loadBalancer = payment.NewDefaultLoadBalancer(s.entClient, key)
	s.resumeService = NewPaymentResumeService(key)
	return s, &PaymentConfig{MaxAmount: 500, BalanceRetailPricingEnabled: true, BalanceRetailQuoteTTLSeconds: 900}
}

func TestGelvAlipayCashQuotesLimitsAndBinding(t *testing.T) {
	s, cfg := newArbitraryAlipayQuoteService(t)
	ctx := context.Background()
	for _, tc := range []struct{ cash, credit float64 }{{0.01, 0.01}, {12.34, 12.27}, {50, 49.70}} {
		req := CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: tc.cash}
		out, err := s.quoteGelvPayment(ctx, req, cfg)
		require.NoError(t, err)
		require.Equal(t, tc.cash, out.RetailQuote.PayAmount)
		require.Equal(t, tc.credit, out.RetailQuote.CreditedAmountUSD)
		require.Equal(t, map[string]float64{gelvAlipayCheckout: tc.credit}, out.RetailQuote.CreditByChannel)
		require.True(t, out.RetailQuote.CashAmountInput)
		claims, err := s.parseRetailQuote(out.QuoteToken)
		require.NoError(t, err)
		require.Equal(t, "60", claims.ProviderIdentity["gelv_alipay_fee_bps"])
		require.NoError(t, validateRetailQuoteRequest(claims, req))
		req.Amount += 0.01
		require.Error(t, validateRetailQuoteRequest(claims, req))
	}
	for _, cash := range []float64{50.01, 100, 0, -1, 0.001, 12.345} {
		_, err := s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: cash}, cfg)
		require.Error(t, err)
	}
	cfg.MinAmount = 10
	_, err := s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, Amount: 0.01}, cfg)
	require.Error(t, err)
	limits, err := s.configService.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	alipay := limits.Methods[payment.TypeGelvAlipay]
	require.Equal(t, 0.01, alipay.SingleMin)
	require.Equal(t, 50.0, alipay.SingleMax)
	require.Equal(t, int64(60), *alipay.FeeRateBPS)
	raw, err := json.Marshal(alipay)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "topup_options")
	require.Len(t, limits.Methods[payment.TypeGelv].TopupOptions, 6)
}

func TestGelvAlipayHistoricalFulfillmentAboveNewCap(t *testing.T) {
	s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusPending, "60")
	ctx := context.Background()
	q := PaymentOrderRetailQuote(o)
	q.PayAmount, q.ProductID, q.CreditedAmountUSD, q.BaseAmountCNY = 100, "6", 99.4, 99.4
	q.CreditByChannel = map[string]float64{gelvAlipayCheckout: 99.4}
	o.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(q)
	o.ProviderSnapshot["gelv_product_id"], o.ProviderSnapshot["gelv_product_ids"] = "6", "3,4,5,6,7,8"
	var err error
	o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetAmount(99.4).SetPayAmount(100).SetProviderSnapshot(o.ProviderSnapshot).Save(ctx)
	require.NoError(t, err)
	m := gelvAlipayMetadata(o)
	m["amount_minor"], m["product_id"] = "10000", "6"
	p := &paymentOrderLifecycleQueryProvider{key: payment.TypeGelv, resp: &payment.QueryOrderResponse{TradeNo: o.PaymentTradeNo, Status: payment.ProviderStatusPaid, Amount: 100, Metadata: m}}
	for i := 0; i < 3; i++ {
		current, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
		require.NoError(t, err)
		_, err = s.reconcileGelvOrderWithProvider(ctx, current, p)
		require.NoError(t, err)
	}
	saved, err := s.entClient.PaymentOrder.Get(ctx, o.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, saved.Status)
	require.Equal(t, 99.4, users.getByIDUser.Balance)
	require.Equal(t, 1, codes.createCalls)
	require.Len(t, codes.useCalls, 1)
}

func TestGelvAlipayNewCashFulfillmentAndRefundGuard(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(strconv.FormatBool(held), func(t *testing.T) {
			s, o, users, codes := gelvAlipayServiceOrder(t, OrderStatusPending, "60")
			ctx := context.Background()
			q := PaymentOrderRetailQuote(o)
			q.CashAmountInput = true
			q.ProductID = ""
			q.PayAmount = 12.34
			q.CreditedAmountUSD = 12.27
			q.BaseAmountCNY = 12.27
			q.CreditByChannel = map[string]float64{gelvAlipayCheckout: 12.27}
			o.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(q)
			o.ProviderSnapshot["gelv_product_id"] = ""
			var err error
			o, err = s.entClient.PaymentOrder.UpdateOneID(o.ID).SetAmount(12.27).SetPayAmount(12.34).SetProviderSnapshot(o.ProviderSnapshot).Save(ctx)
			require.NoError(t, err)
			m := gelvAlipayMetadata(o)
			m["amount_minor"], m["product_id"] = "1234", ""
			m["refund_pending"] = strconv.FormatBool(held)
			if held {
				require.Error(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 12.34, payment.TypeGelv, m))
				require.Zero(t, users.getByIDUser.Balance)
				require.Zero(t, codes.createCalls)
				return
			}
			for i := 0; i < 3; i++ {
				require.NoError(t, s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, 12.34, payment.TypeGelv, m))
			}
			require.Equal(t, 12.27, users.getByIDUser.Balance)
			require.Equal(t, 1, codes.createCalls)
		})
	}
}

type arbitraryAlipayTransport func(*http.Request) (*http.Response, error)

func (f arbitraryAlipayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGelvAlipayCashOrderPersistsNetAndReplaysOnce(t *testing.T) {
	s, cfg := newArbitraryAlipayQuoteService(t)
	ctx := context.Background()
	cfg.MaxPendingOrders = 5
	req := CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: 12.34}
	quote, err := s.quoteGelvPayment(ctx, req, cfg)
	require.NoError(t, err)
	req.QuoteToken = quote.QuoteToken
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	creates := 0
	externalID := "abcdef0123456789abcdef01"
	http.DefaultTransport = arbitraryAlipayTransport(func(r *http.Request) (*http.Response, error) {
		creates++
		require.Equal(t, "https://alar1c.cn/api/partner/v1/orders", r.URL.String())
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, fmt.Sprintf(`{"partnerOrderId":%q,"amount":1234,"paymentChannel":"alipay_web"}`, quote.RetailQuote.CheckoutReference), string(body))
		raw, err := json.Marshal(map[string]any{"checkoutUrl": "https://alar1c.cn/partner-pay/#id=" + externalID + "&token=" + strings.Repeat("a", 64), "order": map[string]any{
			"id": externalID, "partnerOrderId": quote.RetailQuote.CheckoutReference, "amount": 1234, "currency": "CNY", "productId": "", "status": "pending", "refunded": 0, "checkoutChannel": "alipay_web", "paymentChannel": "alipay_web",
		}})
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
	user := s.userRepo.(*mockUserRepo).getByIDUser
	for i := 0; i < 3; i++ {
		result, err := s.createRetailQuotedOrder(ctx, req, cfg, user)
		require.NoError(t, err)
		require.Equal(t, 12.27, result.Amount)
		o, err := s.entClient.PaymentOrder.Get(ctx, result.OrderID)
		require.NoError(t, err)
		require.Equal(t, 12.27, o.Amount)
		require.Equal(t, 12.34, o.PayAmount)
		require.Zero(t, o.FeeRate)
		require.Equal(t, "60", o.ProviderSnapshot["gelv_alipay_fee_bps"])
	}
	require.Equal(t, 1, creates)
	count, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count) // one fixture and one new order
}

func TestGelvAlipayUnusedHistoricalQuoteCannotCreateAboveCap(t *testing.T) {
	s, cfg := newArbitraryAlipayQuoteService(t)
	ctx := context.Background()
	out, err := s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, Amount: 10}, cfg)
	require.NoError(t, err)
	claims, err := s.parseRetailQuote(out.QuoteToken)
	require.NoError(t, err)
	claims.Quote.CashAmountInput = false
	claims.Quote.PayAmount = 100
	claims.Quote.CreditedAmountUSD = 99.4
	claims.Quote.BaseAmountCNY = 99.4
	claims.Quote.CreditByChannel = map[string]float64{gelvAlipayCheckout: 99.4}
	claims.Quote.ProductID = "6"
	token, err := s.signRetailQuote(*claims)
	require.NoError(t, err)
	count, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	_, err = s.createRetailQuotedOrder(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: 99.4, QuoteToken: token}, cfg, s.userRepo.(*mockUserRepo).getByIDUser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "0.01 to 50")
	after, err := s.entClient.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, count, after)
}

func TestGelvAlipayAdvertisedLimitsIntersectExistingConfig(t *testing.T) {
	s, _ := newArbitraryAlipayQuoteService(t)
	ctx := context.Background()
	s.configService.settingRepo = &paymentConfigSettingRepoStub{values: map[string]string{SettingMinRechargeAmount: "2.001", SettingMaxRechargeAmount: "40.999"}}
	inst, err := s.entClient.PaymentProviderInstance.Query().Only(ctx)
	require.NoError(t, err)
	_, err = s.entClient.PaymentProviderInstance.UpdateOneID(inst.ID).SetLimits(`{"gelv_alipay":{"single_min":1,"single_max":45}}`).Save(ctx)
	require.NoError(t, err)
	direct, err := s.configService.GetMethodLimits(ctx, []string{payment.TypeGelvAlipay})
	require.NoError(t, err)
	require.Len(t, direct, 1)
	require.Equal(t, 2.01, direct[0].SingleMin)
	require.Equal(t, 40.99, direct[0].SingleMax)
	available, err := s.configService.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	require.Equal(t, direct[0], available.Methods[payment.TypeGelvAlipay])
}

func TestGelvAlipayHistoricalOrderReplayIgnoresNewCap(t *testing.T) {
	s, o, _, _ := gelvAlipayServiceOrder(t, OrderStatusCompleted, "60")
	ctx := context.Background()
	s.resumeService = NewPaymentResumeService([]byte(strings.Repeat("q", 32)))
	q := PaymentOrderRetailQuote(o)
	q.PayAmount = 100
	q.ProductID = "6"
	q.CreditedAmountUSD = 99.4
	q.BaseAmountCNY = 99.4
	q.CreditByChannel = map[string]float64{gelvAlipayCheckout: 99.4}
	q.ExpiresAt = time.Now().Add(-time.Hour)
	o.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(q)
	o, err := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetAmount(99.4).SetPayAmount(100).SetProviderSnapshot(o.ProviderSnapshot).Save(ctx)
	require.NoError(t, err)
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: o.UserID, PaymentType: payment.TypeGelvAlipay, ProviderInstanceID: "1", ProviderKey: payment.TypeGelv, Quote: q}
	token, err := s.signRetailQuote(claims)
	require.NoError(t, err)
	req := CreateOrderRequest{UserID: o.UserID, PaymentType: payment.TypeGelvAlipay, OrderType: payment.OrderTypeBalance, Amount: 99.4, QuoteToken: token}
	result, err := s.createRetailQuotedOrder(ctx, req, &PaymentConfig{}, s.userRepo.(*mockUserRepo).getByIDUser)
	require.NoError(t, err)
	require.Equal(t, o.ID, result.OrderID)
	require.Equal(t, 99.4, result.Amount)
}

func TestGelvAlipayExistingOneCNYMinimumNeedsNoConfigWrite(t *testing.T) {
	s, _ := newArbitraryAlipayQuoteService(t)
	ctx := context.Background()
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingMinRechargeAmount: "1", SettingMaxRechargeAmount: "0"}}
	s.configService.settingRepo = repo
	cfg, err := s.configService.GetPaymentConfig(ctx)
	require.NoError(t, err)
	limits, err := s.configService.GetAvailableMethodLimits(ctx)
	require.NoError(t, err)
	require.Equal(t, 1.0, limits.Methods[payment.TypeGelvAlipay].SingleMin)
	require.Equal(t, 50.0, limits.Methods[payment.TypeGelvAlipay].SingleMax)
	for _, cash := range []float64{1, 12.34, 50} {
		_, err := s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, Amount: cash}, cfg)
		require.NoError(t, err)
	}
	for _, cash := range []float64{0.99, 50.01} {
		_, err := s.quoteGelvPayment(ctx, CreateOrderRequest{UserID: 1, PaymentType: payment.TypeGelvAlipay, Amount: cash}, cfg)
		require.Error(t, err)
	}
	require.Nil(t, repo.updates, "reading limits and quoting must not write settings")
}
