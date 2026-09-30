//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func retailQuoteFixture(t *testing.T, currency string) *RetailQuote {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	cfg := &PaymentConfig{BalanceRetailCostRate: 2, BalanceRetailFixedCostGBP: .25, BalanceRetailQuoteTTLSeconds: 900}
	quote, err := calculateRetailQuote(25, currency, cfg, &RetailFX{Rates: map[string]float64{"GBP": 1, "USD": 1.25, "CNY": 7}, Source: "manual", AsOf: now}, now)
	require.NoError(t, err)
	quote.CheckoutReference = "sub2_0123456789abcdef0123456789abcdef"
	quote.ProductID = "0123456789abcdef01234567"
	return quote
}
func TestRetailQuoteSameGBPProductPriceAcrossGateways(t *testing.T) {
	for currency, pay := range map[string]float64{"GBP": 20.67, "USD": 25.84, "CNY": 144.69} {
		q := retailQuoteFixture(t, currency)
		require.Equal(t, 25.0, q.CreditedAmountUSD)
		require.Equal(t, 20.0, q.BaseAmountGBP)
		require.Equal(t, .67, q.IncludedCostGBP)
		require.Equal(t, 20.67, q.TotalAmountGBP)
		require.Equal(t, pay, q.PayAmount)
	}
}
func TestRetailQuoteRejectsInvalidMoneyAndCostPolicy(t *testing.T) {
	now := time.Now()
	fx := &RetailFX{Rates: map[string]float64{"GBP": 1, "USD": 1.25, "CNY": 7}}
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1), 1.001} {
		_, err := calculateRetailQuote(amount, "GBP", &PaymentConfig{BalanceRetailQuoteTTLSeconds: 900}, fx, now)
		require.Error(t, err)
	}
	for _, rate := range []float64{-1, 100, math.NaN(), math.Inf(1)} {
		_, err := calculateRetailQuote(1, "GBP", &PaymentConfig{BalanceRetailCostRate: rate, BalanceRetailQuoteTTLSeconds: 900}, fx, now)
		require.Error(t, err)
	}
	_, err := calculateRetailQuote(25, "JPY", &PaymentConfig{BalanceRetailQuoteTTLSeconds: 900}, fx, now)
	require.Error(t, err)
}
func TestRetailQuoteTokenCannotCrossUserAmountMethodOrPurpose(t *testing.T) {
	s := &PaymentService{resumeService: NewPaymentResumeService([]byte(strings.Repeat("k", 32)))}
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: 4, PaymentType: "squarespace", ProviderInstanceID: "1", ProviderKey: "squarespace", Quote: retailQuoteFixture(t, "GBP")}
	token, err := s.signRetailQuote(claims)
	require.NoError(t, err)
	decoded, err := s.parseRetailQuote(token)
	require.NoError(t, err)
	req := CreateOrderRequest{UserID: 4, Amount: 25, PaymentType: "squarespace", OrderType: "balance"}
	require.NoError(t, validateRetailQuoteRequest(decoded, req))
	for _, changed := range []CreateOrderRequest{{UserID: 5, Amount: 25, PaymentType: "squarespace", OrderType: "balance"}, {UserID: 4, Amount: 24, PaymentType: "squarespace", OrderType: "balance"}, {UserID: 4, Amount: 25, PaymentType: "stripe", OrderType: "balance"}, {UserID: 4, Amount: 25, PaymentType: "squarespace", OrderType: "subscription"}} {
		require.Error(t, validateRetailQuoteRequest(decoded, changed))
	}
	parts := strings.Split(token, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	tampered := strings.Replace(string(raw), "20.67", "20.68", 1)
	_, err = s.parseRetailQuote(base64.RawURLEncoding.EncodeToString([]byte(tampered)) + "." + parts[1])
	require.Error(t, err)
	claims.Purpose = "payment_resume"
	other, err := s.signRetailQuote(claims)
	require.NoError(t, err)
	_, err = s.parseRetailQuote(other)
	require.Error(t, err)
}
func TestRetailCheckoutReferencesUseCryptoEntropy(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		reference, err := newRetailCheckoutReference()
		require.NoError(t, err)
		require.Len(t, reference, 37)
		require.False(t, seen[reference])
		seen[reference] = true
	}
}
func TestRetailOrderSnapshotAndExactPennyChecks(t *testing.T) {
	q := retailQuoteFixture(t, "GBP")
	o := &dbent.PaymentOrder{Amount: 25, PayAmount: q.PayAmount, ProviderKey: retailTestString("squarespace"), ProviderSnapshot: map[string]any{"retail_quote": retailQuoteSnapshot(q), "currency": "GBP", "merchant_id": "website", "product_id": "0123456789abcdef01234567"}}
	require.Equal(t, q, PaymentOrderRetailQuote(o))
	require.True(t, paymentOrderAmountMatches(o, 20.67))
	require.False(t, paymentOrderAmountMatches(o, 20.66))
	require.False(t, paymentOrderAmountMatches(o, 20.68))
	require.False(t, paymentOrderAmountMatches(o, 20.67001))
	meta := map[string]string{"website_id": "website", "product_id": "0123456789abcdef01234567", "currency": "GBP", "checkout_reference": q.CheckoutReference, "squarespace_paid_on": q.IssuedAt.Add(time.Minute).Format(time.RFC3339)}
	require.NoError(t, validateSquarespaceRetailMetadata(o, meta))
	meta["squarespace_paid_on"] = q.ExpiresAt.Add(time.Second).Format(time.RFC3339)
	require.Error(t, validateSquarespaceRetailMetadata(o, meta))
	delete(meta, "squarespace_paid_on")
	require.Error(t, validateSquarespaceRetailMetadata(o, meta))
}
func TestRetailQuotedOrderReplayReturnsSameOrderEvenAfterCompletion(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().SetEmail("retail-replay@example.com").SetPasswordHash("hash").SetUsername("retail").Save(ctx)
	require.NoError(t, err)
	q := retailQuoteFixture(t, "GBP")
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: user.ID, PaymentType: "squarespace", ProviderInstanceID: "1", ProviderKey: "squarespace", Quote: q}
	s := &PaymentService{entClient: client, resumeService: NewPaymentResumeService([]byte(strings.Repeat("k", 32)))}
	token, err := s.signRetailQuote(claims)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(25).SetPayAmount(q.PayAmount).SetFeeRate(0).SetRechargeCode("RETAIL-REPLAY").SetOutTradeNo(q.CheckoutReference).SetPaymentType("squarespace").SetPaymentTradeNo("external-order").SetOrderType("balance").SetStatus(OrderStatusCompleted).SetExpiresAt(q.ExpiresAt).SetClientIP("127.0.0.1").SetSrcHost("example.com").SetProviderKey("squarespace").SetProviderInstanceID("1").SetProviderSnapshot(map[string]any{"retail_quote": retailQuoteSnapshot(q), "currency": "GBP"}).Save(ctx)
	require.NoError(t, err)
	req := CreateOrderRequest{UserID: user.ID, Amount: 25, PaymentType: "squarespace", OrderType: "balance", QuoteToken: token}
	for i := 0; i < 3; i++ {
		result, err := s.createRetailQuotedOrder(ctx, req, &PaymentConfig{BalanceRetailPricingEnabled: true, MerchantTestUserIDs: []int64{user.ID}}, &User{ID: user.ID})
		require.NoError(t, err)
		require.Equal(t, order.ID, result.OrderID)
		require.Equal(t, OrderStatusCompleted, result.Status)
		require.Equal(t, 25.0, result.Amount)
	}
	count, err := client.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
func TestPaymentProviderCredentialsEncryptedAndTamperingFailsClosed(t *testing.T) {
	s := &PaymentConfigService{encryptionKey: []byte(strings.Repeat("k", 32))}
	config := map[string]string{"secretKey": "payment-credential-canary", "websiteId": "public-website"}
	first, err := s.encryptConfig(config)
	require.NoError(t, err)
	second, err := s.encryptConfig(config)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.NotContains(t, first, "payment-credential-canary")
	decoded, err := s.decryptConfig(first)
	require.NoError(t, err)
	require.Equal(t, config, decoded)
	legacy, _ := json.Marshal(config)
	decoded, err = s.decryptConfig(string(legacy))
	require.NoError(t, err)
	require.Equal(t, config, decoded)
	_, err = s.decryptConfig(first[:len(first)-2] + "!!")
	require.Error(t, err)
	_, err = (&PaymentConfigService{}).encryptConfig(config)
	require.Error(t, err)
	require.True(t, isSensitiveProviderConfigField("squarespace", "CLIENTSECRET"))
	require.True(t, hasPendingOrderProtectedConfigChange("squarespace", map[string]string{"websiteId": "first"}, map[string]string{"websiteId": "second"}))
}
func TestRetailConfigDefaultsOffAndUpdateValidation(t *testing.T) {
	s := &PaymentConfigService{}
	cfg := s.parsePaymentConfig(map[string]string{})
	require.False(t, cfg.BalanceRetailPricingEnabled)
	require.Equal(t, 2.0, cfg.BalanceRetailCostRate)
	require.Equal(t, .25, cfg.BalanceRetailFixedCostGBP)
	require.Equal(t, 120, cfg.BalanceRetailFXMaxAgeHours)
	negative := -1.0
	require.Error(t, retailPaymentConfigUpdates(UpdatePaymentConfigRequest{BalanceRetailCostRate: &negative}, map[string]string{}))
	onehundred := 100.0
	require.Error(t, retailPaymentConfigUpdates(UpdatePaymentConfigRequest{BalanceRetailCostRate: &onehundred}, map[string]string{}))
}
func retailTestString(s string) *string { return &s }

func TestSquarespaceRefundedOrderCannotRedeemWhileFulfillmentInFlight(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().SetEmail("refund-guard@example.com").SetPasswordHash("hash").SetUsername("guard").Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(25).SetPayAmount(20.67).SetFeeRate(0).SetRechargeCode("SQUARESPACE-GUARD").SetOutTradeNo("sub2_guard").SetPaymentType("squarespace").SetPaymentTradeNo("external-order").SetOrderType("balance").SetStatus(OrderStatusRecharging).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.com").Save(ctx)
	require.NoError(t, err)
	guardedCtx := context.WithValue(ctx, paymentRedeemGuardKey{}, paymentRedeemGuard{OrderID: order.ID, LeaseVersion: order.UpdatedAt})
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundPending).Save(ctx)
	require.NoError(t, err)
	users := &mockUserRepo{getByIDUser: &User{ID: user.ID, Email: user.Email, Balance: 0}}
	credited := false
	users.updateBalanceFn = func(context.Context, int64, float64) error { credited = true; return nil }
	codes := &paymentOrderLifecycleRedeemRepo{codesByCode: map[string]*RedeemCode{order.RechargeCode: {ID: 1, Code: order.RechargeCode, Type: RedeemTypeBalance, Value: 25, Status: StatusUnused}}}
	redeem := NewRedeemService(codes, users, nil, nil, nil, client, nil, nil)
	_, err = redeem.redeemForPaymentFulfillment(guardedCtx, user.ID, order.RechargeCode)
	require.Error(t, err)
	require.False(t, credited)
	require.Empty(t, codes.useCalls)
}

func TestSquarespaceProviderConfigRejectsCredentialKeys(t *testing.T) {
	require.NoError(t, validateSquarespacePublicConfig(map[string]string{"websiteId": "website", "payLinkUrl": "https://example.squarespace.com/pay-link/", "currency": "GBP", "paymentClaimMode": "receipt_otp"}))
	for _, key := range []string{"clientSecret", "accessToken", "refreshToken", "apiKey"} {
		require.Error(t, validateSquarespacePublicConfig(map[string]string{key: "credential-canary"}))
	}
	require.Error(t, validateSquarespacePublicConfig(map[string]string{"paymentClaimMode": "email"}))
}

func TestSquarespaceRetailWindowRejectsMixedOldAndCurrentPayments(t *testing.T) {
	q := retailQuoteFixture(t, "GBP")
	latest := q.IssuedAt.Add(time.Second).Format(time.RFC3339)
	metadata := map[string]string{"squarespace_paid_on": latest, "upstream_paid_on": `["` + q.IssuedAt.Add(-time.Second).Format(time.RFC3339) + `","` + latest + `"]`}
	require.Error(t, ValidateSquarespaceRetailPaymentTimes(q, metadata))
	metadata["upstream_paid_on"] = `["` + latest + `"]`
	require.NoError(t, ValidateSquarespaceRetailPaymentTimes(q, metadata))
}

func TestMerchantPaymentAccessIsLimitedToSquareBalanceAndNeverPublicIDs(t *testing.T) {
	cfg := &PaymentConfig{Enabled: false, MerchantTestUserIDs: []int64{77}}
	require.True(t, paymentRequestEnabled(cfg, CreateOrderRequest{UserID: 77, PaymentType: "squarespace", OrderType: "balance"}))
	for _, req := range []CreateOrderRequest{{UserID: 78, PaymentType: "squarespace", OrderType: "balance"}, {UserID: 77, PaymentType: "stripe", OrderType: "balance"}, {UserID: 77, PaymentType: "squarespace", OrderType: "subscription"}} {
		require.False(t, paymentRequestEnabled(cfg, req))
	}
	selection := &payment.InstanceSelection{ProviderKey: "squarespace", Config: map[string]string{}}
	require.NoError(t, requireSquarespaceProductionAccess(cfg, 77, selection))
	require.Error(t, requireSquarespaceProductionAccess(cfg, 78, selection))
	selection.Config["productionApproved"] = "true"
	require.NoError(t, requireSquarespaceProductionAccess(cfg, 78, selection))
	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "merchant_test_user_ids")
	updates := map[string]string{}
	require.NoError(t, retailPaymentConfigUpdates(UpdatePaymentConfigRequest{MerchantTestUserIDs: []int64{77}}, updates))
	require.Equal(t, "[77]", updates[SettingMerchantTestUserIDs])
	require.Error(t, retailPaymentConfigUpdates(UpdatePaymentConfigRequest{MerchantTestUserIDs: []int64{77, 77}}, updates))
	require.NoError(t, retailPaymentConfigUpdates(UpdatePaymentConfigRequest{MerchantTestUserIDs: []int64{}}, updates))
	require.Equal(t, "[]", updates[SettingMerchantTestUserIDs])
}

func TestSquarespaceRefundStopsNewAffiliateRebateFromStaleFulfillment(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().SetEmail("affiliate-refund-guard@example.com").SetPasswordHash("hash").SetUsername("affiliateguard").Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName(user.Username).SetAmount(25).SetPayAmount(20.67).SetFeeRate(0).SetRechargeCode("AFFILIATE-GUARD").SetOutTradeNo("sub2_affiliate_guard").SetPaymentType("squarespace").SetProviderKey("squarespace").SetPaymentTradeNo("external-order").SetOrderType("balance").SetStatus(OrderStatusRecharging).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("example.com").Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundPending).Save(ctx)
	require.NoError(t, err)
	s := &PaymentService{entClient: client, affiliateService: &AffiliateService{}}
	require.NoError(t, s.applyAffiliateRebateForOrder(ctx, order))
	count, err := client.PaymentAuditLog.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestMerchantPaymentAllowlistMalformedSettingsFailClosed(t *testing.T) {
	for _, invalid := range []string{`[77,"bad"]`, `[77,0]`, `[77,77]`, `77`, `{"uid":77}`} {
		cfg := (&PaymentConfigService{}).parsePaymentConfig(map[string]string{SettingMerchantTestUserIDs: invalid})
		require.False(t, merchantTestUser(cfg, 77))
	}
}

func TestSquarespaceProductSnapshotIsFrozenIntoQuoteIdentity(t *testing.T) {
	sel := &payment.InstanceSelection{ProviderKey: "squarespace", InstanceID: "1", Config: map[string]string{"websiteId": "website", "productId": "0123456789abcdef01234567", "currency": "GBP"}}
	q := retailQuoteFixture(t, "GBP")
	q.ProductID = "0123456789abcdef01234567"
	snapshot := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{RetailQuote: q})
	require.Equal(t, "0123456789abcdef01234567", snapshot["product_id"])
	order := &dbent.PaymentOrder{ProviderSnapshot: snapshot}
	require.Equal(t, "0123456789abcdef01234567", PaymentOrderSquarespaceProductID(order))
	require.Equal(t, "0123456789abcdef01234567", PaymentOrderRetailQuote(order).ProductID)
	sel.Config["productId"] = "abcdef0123456789abcdef01"
	newIdentity := buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{})
	require.Equal(t, "abcdef0123456789abcdef01", newIdentity["product_id"])
	require.Equal(t, "0123456789abcdef01234567", order.ProviderSnapshot["product_id"])
}

func TestSquarespaceProductConfigCanonicalizesAndRejectsMalformedID(t *testing.T) {
	config := map[string]string{"productId": " 0123456789ABCDEF01234567 "}
	require.NoError(t, validateSquarespacePublicConfig(config))
	require.Equal(t, "0123456789abcdef01234567", config["productId"])
	require.Error(t, validateSquarespacePublicConfig(map[string]string{"productId": "other-product"}))
}
