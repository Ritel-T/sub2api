package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const retailQuotePurpose = "sub2api-retail-quote-v1"
const paymentRetailQuoteSigningKeyEnv = "PAYMENT_RETAIL_QUOTE_SIGNING_KEY"

// RetailQuote is the immutable price of site balance credits. The historical
// credited_amount_usd name denotes displayed '$' credit units, not cash USD.
// New products use one CNY of principal per credit unit. The GBP retail price
// includes site costs identically for every method; cash currency conversion
// never changes the credited quantity. Optional pricing fields remain absent
// on historical USD-priced quotes so their stored JSON and proof hashes survive.
type RetailQuote struct {
	CreditedAmountUSD    float64            `json:"credited_amount_usd"`
	BaseAmountGBP        float64            `json:"base_amount_gbp"`
	IncludedCostGBP      float64            `json:"included_cost_gbp"`
	TotalAmountGBP       float64            `json:"total_amount_gbp"`
	PayAmount            float64            `json:"pay_amount"`
	Currency             string             `json:"currency"`
	FX                   map[string]float64 `json:"fx"`
	FXSource             string             `json:"fx_source"`
	FXAsOf               time.Time          `json:"fx_asof"`
	IssuedAt             time.Time          `json:"issued_at"`
	ExpiresAt            time.Time          `json:"expires_at"`
	CheckoutReference    string             `json:"checkout_reference"`
	CostRate             float64            `json:"cost_rate"`
	FixedCostGBP         float64            `json:"fixed_cost_gbp"`
	PaymentClaimMode     string             `json:"payment_claim_mode,omitempty"`
	ProductID            string             `json:"product_id,omitempty"`
	OrderScopeMode       string             `json:"order_scope_mode,omitempty"`
	ExpectedServiceName  string             `json:"expected_service_name,omitempty"`
	Purpose              string             `json:"purpose,omitempty"`
	PricingBasisCurrency string             `json:"pricing_basis_currency,omitempty"`
	BaseAmountCNY        float64            `json:"base_amount_cny,omitempty"`
}
type RetailQuoteResponse struct {
	QuoteToken  string       `json:"quote_token"`
	RetailQuote *RetailQuote `json:"retail_quote"`
}
type retailQuoteClaims struct {
	Purpose            string         `json:"purpose"`
	UserID             int64          `json:"user_id"`
	PaymentType        string         `json:"payment_type"`
	ProviderInstanceID string         `json:"provider_instance_id"`
	ProviderKey        string         `json:"provider_key"`
	ProviderIdentity   map[string]any `json:"provider_identity"`
	Quote              *RetailQuote   `json:"retail_quote"`
}

func (s *PaymentService) QuotePayment(ctx context.Context, req CreateOrderRequest) (*RetailQuoteResponse, error) {
	if req.OrderType == "" {
		req.OrderType = payment.OrderTypeBalance
	}
	cfg, err := s.configService.GetPaymentConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !paymentRequestEnabled(cfg, req) || cfg.BalanceDisabled || !cfg.BalanceRetailPricingEnabled {
		return nil, infraerrors.Forbidden("RETAIL_PRICING_DISABLED", "uniform balance product pricing is disabled")
	}
	if req.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("RETAIL_QUOTE_BALANCE_ONLY", "retail quotes currently support balance products")
	}
	req.PaymentType = NormalizeVisibleMethod(req.PaymentType)
	if _, err = s.validateOrderInput(ctx, req, cfg); err != nil {
		return nil, err
	}
	if _, err = payment.AmountToMinorUnit(strconv.FormatFloat(req.Amount, 'f', -1, 64), "CNY"); err != nil {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", err.Error())
	}
	user, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != payment.EntityStatusActive {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user account is disabled")
	}
	currency, err := s.configService.ValidateMethodCurrencyConsistency(ctx, req.PaymentType)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	fx, err := ResolveRetailFX(ctx, cfg, now)
	if err != nil {
		return nil, err
	}
	quote, err := calculateRetailQuote(req.Amount, currency, cfg, fx, now)
	if err != nil {
		return nil, err
	}
	sel, err := s.selectCreateOrderInstance(ctx, req, cfg, quote.PayAmount)
	if err != nil {
		return nil, err
	}
	if err = requireSquarespaceProductionAccess(cfg, req.UserID, sel); err != nil {
		return nil, err
	}
	if err = s.validateSelectedCreateOrderInstance(ctx, req, sel); err != nil {
		return nil, err
	}
	if paymentProviderConfigCurrency(sel.ProviderKey, sel.Config) != quote.Currency {
		return nil, infraerrors.Conflict("PAYMENT_CURRENCY_CHANGED", "payment method currency changed; request a new quote")
	}
	if sel.ProviderKey == "squarespace" {
		quote.OrderScopeMode = strings.TrimSpace(sel.Config["orderScopeMode"])
		if quote.OrderScopeMode == "" {
			quote.OrderScopeMode = provider.SquarespaceScopeFixedProduct
		}
		switch quote.OrderScopeMode {
		case provider.SquarespaceScopeDedicatedSiteService:
			if sel.Config["paymentPurpose"] != provider.SquarespaceBalanceTopupPurpose || sel.Config["expectedServiceName"] != provider.SquarespaceExpectedServiceName {
				return nil, infraerrors.BadRequest("INVALID_SQUARESPACE_SCOPE", "dedicated website purpose and service must be explicit")
			}
			quote.Purpose = sel.Config["paymentPurpose"]
			quote.ExpectedServiceName = sel.Config["expectedServiceName"]
		case provider.SquarespaceScopeFixedProduct:
			quote.ProductID, err = canonicalSquarespaceProductID(sel.Config["productId"])
			if err != nil {
				return nil, infraerrors.ServiceUnavailable("SQUARESPACE_PRODUCT_NOT_CONFIGURED", "the payment product is not configured")
			}
		default:
			return nil, infraerrors.BadRequest("INVALID_SQUARESPACE_SCOPE", "unsupported payment website scope")
		}
		quote.PaymentClaimMode = strings.TrimSpace(sel.Config["paymentClaimMode"])
		if quote.PaymentClaimMode == "" {
			quote.PaymentClaimMode = "receipt_otp"
		}
		if quote.PaymentClaimMode != "receipt_otp" && quote.PaymentClaimMode != "reference" {
			return nil, infraerrors.BadRequest("INVALID_PAYMENT_CLAIM_MODE", "unsupported Squarespace claim mode")
		}
	}
	quote.CheckoutReference, err = newRetailCheckoutReference()
	if err != nil {
		return nil, err
	}
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: req.UserID, PaymentType: req.PaymentType, ProviderInstanceID: sel.InstanceID, ProviderKey: sel.ProviderKey, ProviderIdentity: buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{}), Quote: quote}
	token, err := s.signRetailQuote(claims)
	if err != nil {
		return nil, err
	}
	return &RetailQuoteResponse{QuoteToken: token, RetailQuote: quote}, nil
}

func calculateRetailQuote(credits float64, currency string, cfg *PaymentConfig, fx *RetailFX, now time.Time) (*RetailQuote, error) {
	bad := func(message string) (*RetailQuote, error) {
		return nil, infraerrors.BadRequest("INVALID_RETAIL_QUOTE", message)
	}
	if cfg == nil || fx == nil || credits <= 0 || math.IsNaN(credits) || math.IsInf(credits, 0) {
		return bad("invalid retail quote input")
	}
	if _, err := payment.AmountToMinorUnit(strconv.FormatFloat(credits, 'f', -1, 64), "CNY"); err != nil {
		return bad(err.Error())
	}
	if currency != "GBP" && currency != "USD" && currency != "CNY" {
		return bad("uniform retail pricing currently supports GBP, USD and CNY gateways")
	}
	if fx.Rates["GBP"] != 1 || !validRetailNumber(fx.Rates["CNY"]) || !validRetailNumber(fx.Rates["USD"]) || !validRetailNumber(fx.Rates[currency]) {
		return bad("FX conversion rate is unavailable")
	}
	rate, fixed := cfg.BalanceRetailCostRate, cfg.BalanceRetailFixedCostGBP
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate >= 100 || math.IsNaN(fixed) || math.IsInf(fixed, 0) || fixed < 0 {
		return bad("invalid included-cost pricing parameters")
	}
	if cfg.BalanceRetailQuoteTTLSeconds < 60 || cfg.BalanceRetailQuoteTTLSeconds > 3600 {
		return bad("invalid retail quote lifetime")
	}
	base := decimal.NewFromFloat(credits).Div(decimal.NewFromFloat(fx.Rates["CNY"])).RoundUp(2)
	total := base.Add(decimal.NewFromFloat(fixed)).Div(decimal.NewFromInt(1).Sub(decimal.NewFromFloat(rate).Div(decimal.NewFromInt(100)))).RoundUp(2)
	pay := total.Mul(decimal.NewFromFloat(fx.Rates[currency])).RoundUp(2)
	if pay.GreaterThan(decimal.NewFromInt(1000000000)) {
		return bad("retail quote exceeds supported gateway amount")
	}
	copiedFX := map[string]float64{"GBP": 1, "USD": fx.Rates["USD"], "CNY": fx.Rates["CNY"]}
	return &RetailQuote{CreditedAmountUSD: credits, PricingBasisCurrency: "CNY", BaseAmountCNY: credits, BaseAmountGBP: base.InexactFloat64(), IncludedCostGBP: total.Sub(base).InexactFloat64(), TotalAmountGBP: total.InexactFloat64(), PayAmount: pay.InexactFloat64(), Currency: currency, FX: copiedFX, FXSource: fx.Source, FXAsOf: fx.AsOf, IssuedAt: now, ExpiresAt: now.Add(time.Duration(cfg.BalanceRetailQuoteTTLSeconds) * time.Second), CostRate: rate, FixedCostGBP: fixed}, nil
}
func validRetailNumber(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
func newRetailCheckoutReference() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate retail checkout reference: %w", err)
	}
	return "sub2_" + hex.EncodeToString(b), nil
}
func (s *PaymentService) retailQuoteSigningKey() ([]byte, error) {
	if explicit := strings.TrimSpace(os.Getenv(paymentRetailQuoteSigningKeyEnv)); explicit != "" {
		if len(explicit) < 32 {
			return nil, infraerrors.ServiceUnavailable("RETAIL_QUOTE_KEY_UNAVAILABLE", "retail quote signing key must be at least 32 bytes")
		}
		return []byte(explicit), nil
	}
	resume := s.paymentResume()
	if resume == nil || len(resume.signingKey) < 16 {
		return nil, infraerrors.ServiceUnavailable("RETAIL_QUOTE_KEY_UNAVAILABLE", "retail quote signing key is not configured")
	}
	return resume.signingKey, nil
}
func (s *PaymentService) signRetailQuote(claims retailQuoteClaims) (string, error) {
	key, err := s.retailQuoteSigningKey()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(retailQuotePurpose + "\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *PaymentService) parseRetailQuote(token string) (*retailQuoteClaims, error) {
	bad := func() (*retailQuoteClaims, error) {
		return nil, infraerrors.BadRequest("INVALID_RETAIL_QUOTE_TOKEN", "invalid retail quote token")
	}
	if len(token) == 0 || len(token) > 12000 {
		return bad()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return bad()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return bad()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return bad()
	}
	key, err := s.retailQuoteSigningKey()
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(retailQuotePurpose + "\x00"))
	_, _ = mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return bad()
	}
	var claims retailQuoteClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Purpose != retailQuotePurpose || claims.UserID <= 0 || claims.ProviderInstanceID == "" || claims.Quote == nil || claims.Quote.CheckoutReference == "" {
		return bad()
	}
	if _, err := normalizeOrderLookupOutTradeNo(claims.Quote.CheckoutReference); err != nil {
		return bad()
	}
	return &claims, nil
}
func validateRetailQuoteRequest(claims *retailQuoteClaims, req CreateOrderRequest) error {
	if claims.UserID != req.UserID || claims.PaymentType != NormalizeVisibleMethod(req.PaymentType) || req.OrderType != payment.OrderTypeBalance || decimal.NewFromFloat(claims.Quote.CreditedAmountUSD).Cmp(decimal.NewFromFloat(req.Amount)) != 0 {
		return infraerrors.Forbidden("RETAIL_QUOTE_MISMATCH", "quote does not belong to this user, method and balance product")
	}
	return nil
}
func (s *PaymentService) retailQuoteSelection(ctx context.Context, claims *retailQuoteClaims) (*payment.InstanceSelection, error) {
	id, err := strconv.ParseInt(claims.ProviderInstanceID, 10, 64)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_RETAIL_QUOTE_TOKEN", "invalid quote provider")
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, id)
	if err != nil || !inst.Enabled || inst.ProviderKey != claims.ProviderKey {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_PROVIDER_CHANGED", "quoted payment provider is unavailable")
	}
	if !(inst.ProviderKey == payment.TypeStripe && claims.PaymentType == payment.TypeStripe) && !payment.InstanceSupportsType(inst.SupportedTypes, claims.PaymentType) {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_PROVIDER_CHANGED", "quoted provider no longer supports this method")
	}
	config, err := s.configService.decryptConfig(inst.Config)
	if err != nil {
		return nil, err
	}
	if inst.PaymentMode != "" {
		config["paymentMode"] = inst.PaymentMode
	}
	sel := &payment.InstanceSelection{InstanceID: claims.ProviderInstanceID, ProviderKey: inst.ProviderKey, Config: config, SupportedTypes: inst.SupportedTypes, PaymentMode: inst.PaymentMode}
	expected, _ := json.Marshal(claims.ProviderIdentity)
	current, _ := json.Marshal(buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{}))
	if string(expected) != string(current) || paymentProviderConfigCurrency(sel.ProviderKey, sel.Config) != claims.Quote.Currency {
		return nil, infraerrors.Conflict("PAYMENT_PROVIDER_CHANGED", "provider identity changed; request a new quote")
	}
	return sel, nil
}
func (s *PaymentService) createRetailQuotedOrder(ctx context.Context, req CreateOrderRequest, cfg *PaymentConfig, user *User) (*CreateOrderResponse, error) {
	claims, err := s.parseRetailQuote(req.QuoteToken)
	if err != nil {
		return nil, err
	}
	if err = validateRetailQuoteRequest(claims, req); err != nil {
		return nil, err
	}
	req.RetailQuote = claims.Quote
	if claims.ProviderKey == "squarespace" && !merchantTestUser(cfg, req.UserID) {
		selected, err := s.retailQuoteSelection(ctx, claims)
		if err != nil {
			return nil, err
		}
		if err = requireSquarespaceProductionAccess(cfg, req.UserID, selected); err != nil {
			return nil, err
		}
	}
	existing, err := s.entClient.PaymentOrder.Query().Where(paymentorder.OutTradeNo(claims.Quote.CheckoutReference)).Only(ctx)
	if err == nil {
		return s.retailReplayResponse(ctx, existing, req, cfg)
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}
	// An already-created order keeps its original frozen price and can replay
	// above. Unused quotes from the former cash-USD policy must be reissued;
	// accepting them here would create a new order at the superseded price.
	if err = validateCurrentRetailPricingBasis(claims.Quote); err != nil {
		return nil, err
	}
	now := time.Now()
	if !now.Before(claims.Quote.ExpiresAt) || claims.Quote.IssuedAt.After(now.Add(time.Minute)) {
		return nil, infraerrors.BadRequest("RETAIL_QUOTE_EXPIRED", "quote expired; request a new quote")
	}
	sel, err := s.retailQuoteSelection(ctx, claims)
	if err != nil {
		return nil, err
	}
	if err = requireSquarespaceProductionAccess(cfg, req.UserID, sel); err != nil {
		return nil, err
	}
	if err = s.validateSelectedCreateOrderInstance(ctx, req, sel); err != nil {
		return nil, err
	}
	// Limits describe gateway cash, not site-credit units. Existing orders
	// already replayed above preserve their original frozen price.
	instanceID, err := strconv.ParseInt(sel.InstanceID, 10, 64)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_PROVIDER_CHANGED", "invalid quoted provider")
	}
	instance, err := s.entClient.PaymentProviderInstance.Get(ctx, instanceID)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_PROVIDER_CHANGED", "quoted payment provider unavailable")
	}
	if err = validateRetailCashRange(instance.Limits, req.PaymentType, claims.Quote); err != nil {
		return nil, err
	}
	oauth, err := s.maybeBuildWeChatOAuthRequiredResponseForSelection(ctx, req, req.Amount, claims.Quote.PayAmount, 0, sel)
	if err != nil {
		return nil, err
	}
	if oauth != nil {
		oauth.RetailQuote = claims.Quote
		oauth.Currency = claims.Quote.Currency
		return oauth, nil
	}
	order, err := s.createOrderInTx(ctx, req, user, nil, cfg, req.Amount, req.Amount, 0, claims.Quote.PayAmount, sel)
	if err != nil {
		// A concurrent replay may have committed the unique checkout reference.
		existing, lookupErr := s.entClient.PaymentOrder.Query().Where(paymentorder.OutTradeNo(claims.Quote.CheckoutReference)).Only(ctx)
		if lookupErr == nil {
			return s.retailReplayResponse(ctx, existing, req, cfg)
		}
		return nil, err
	}
	result, err := s.invokeProvider(ctx, order, req, cfg, req.Amount, payment.FormatAmountForCurrency(claims.Quote.PayAmount, claims.Quote.Currency), claims.Quote.PayAmount, nil, sel)
	if err != nil {
		_, _ = s.entClient.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusFailed).Save(ctx)
		return nil, err
	}
	return result, nil
}
func validateCurrentRetailPricingBasis(quote *RetailQuote) error {
	if quote == nil || quote.PricingBasisCurrency != "CNY" || !validRetailNumber(quote.BaseAmountCNY) || !validRetailNumber(quote.CreditedAmountUSD) || decimal.NewFromFloat(quote.BaseAmountCNY).Cmp(decimal.NewFromFloat(quote.CreditedAmountUSD)) != 0 {
		return infraerrors.Conflict("RETAIL_PRICING_CHANGED", "balance product pricing changed; request a new quote")
	}
	return nil
}
func validateRetailCashRange(rawLimits, method string, quote *RetailQuote) error {
	if quote == nil {
		return infraerrors.BadRequest("INVALID_RETAIL_QUOTE", "missing payment quote")
	}
	if strings.TrimSpace(rawLimits) == "" {
		return nil
	}
	var limits payment.InstanceLimits
	if json.Unmarshal([]byte(rawLimits), &limits) != nil {
		return infraerrors.ServiceUnavailable("PAYMENT_LIMITS_INVALID", "payment channel limits are invalid")
	}
	key := NormalizeVisibleMethod(method)
	if strings.HasPrefix(key, "stripe") {
		key = "stripe"
	}
	limit := limits[key]
	if math.IsNaN(limit.SingleMin) || math.IsInf(limit.SingleMin, 0) || limit.SingleMin < 0 || math.IsNaN(limit.SingleMax) || math.IsInf(limit.SingleMax, 0) || limit.SingleMax < 0 {
		return infraerrors.ServiceUnavailable("PAYMENT_LIMITS_INVALID", "payment channel limits are invalid")
	}
	if (limit.SingleMin > 0 && quote.PayAmount < limit.SingleMin) || (limit.SingleMax > 0 && quote.PayAmount > limit.SingleMax) {
		return infraerrors.BadRequest("RETAIL_PAYMENT_OUT_OF_RANGE", "payment total is outside this channel's range").WithMetadata(map[string]string{
			"currency": quote.Currency, "min": strconv.FormatFloat(limit.SingleMin, 'f', -1, 64), "max": strconv.FormatFloat(limit.SingleMax, 'f', -1, 64),
		})
	}
	return nil
}

func (s *PaymentService) retailReplayResponse(ctx context.Context, order *dbent.PaymentOrder, req CreateOrderRequest, cfg *PaymentConfig) (*CreateOrderResponse, error) {
	persisted := PaymentOrderRetailQuote(order)
	expected, _ := json.Marshal(req.RetailQuote)
	actual, _ := json.Marshal(persisted)
	if order.UserID != req.UserID || persisted == nil || string(expected) != string(actual) {
		return nil, infraerrors.Forbidden("RETAIL_QUOTE_MISMATCH", "quote is already associated with another order")
	}
	pr := &payment.CreatePaymentResponse{TradeNo: order.PaymentTradeNo, PayURL: psStringValue(order.PayURL), QRCode: psStringValue(order.QrCode), Currency: persisted.Currency}
	sel := &payment.InstanceSelection{InstanceID: psStringValue(order.ProviderInstanceID), ProviderKey: psStringValue(order.ProviderKey)}
	if snap := psOrderProviderSnapshot(order); snap != nil {
		sel.PaymentMode = snap.PaymentMode
	}
	if order.Status == OrderStatusPending && time.Now().Before(order.ExpiresAt) && (sel.ProviderKey == payment.TypeStripe || sel.ProviderKey == payment.TypeAirwallex || pr.PayURL == "" && pr.QRCode == "") {
		claims, err := s.parseRetailQuote(req.QuoteToken)
		if err != nil {
			return nil, err
		}
		selected, err := s.retailQuoteSelection(ctx, claims)
		if err != nil {
			return nil, err
		}
		// Providers use the same out_trade_no/idempotency key: retry restores missing
		// payment details instead of creating another local balance order.
		return s.invokeProvider(ctx, order, req, cfg, order.Amount, payment.FormatAmountForCurrency(order.PayAmount, persisted.Currency), order.PayAmount, nil, selected)
	}
	result := buildCreateOrderResponse(order, req, order.PayAmount, sel, pr, payment.CreatePaymentResultOrderCreated)
	result.Status = order.Status
	if order.Status != OrderStatusPending {
		result.PayURL = ""
		result.QRCode = ""
	}
	return result, nil
}
func retailQuoteSnapshot(quote *RetailQuote) map[string]any {
	encoded, _ := json.Marshal(quote)
	var snapshot map[string]any
	_ = json.Unmarshal(encoded, &snapshot)
	return snapshot
}
func PaymentOrderRetailQuote(order *dbent.PaymentOrder) *RetailQuote {
	if order == nil || order.ProviderSnapshot == nil {
		return nil
	}
	value, ok := order.ProviderSnapshot["retail_quote"]
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var quote RetailQuote
	if json.Unmarshal(encoded, &quote) != nil || quote.CheckoutReference == "" || quote.CreditedAmountUSD <= 0 {
		return nil
	}
	return &quote
}
func paymentOrderAmountMatches(order *dbent.PaymentOrder, paid float64) bool {
	if order == nil || !isValidProviderAmount(paid) {
		return false
	}
	if PaymentOrderRetailQuote(order) != nil || psStringValue(order.ProviderKey) == "squarespace" {
		minor, err := payment.AmountToMinorUnit(strconv.FormatFloat(paid, 'f', -1, 64), PaymentOrderCurrency(order))
		expected, expectedErr := payment.AmountToMinorUnit(strconv.FormatFloat(order.PayAmount, 'f', -1, 64), PaymentOrderCurrency(order))
		return err == nil && expectedErr == nil && minor == expected
	}
	return math.Abs(paid-order.PayAmount) <= paymentAmountToleranceForCurrency(PaymentOrderCurrency(order))
}
func (s *PaymentService) checkDailyLimitUSD(ctx context.Context, tx *dbent.Tx, userID int64, amount, limit float64) error {
	if limit <= 0 {
		return nil
	}
	orders, err := tx.PaymentOrder.Query().Where(paymentorder.UserIDEQ(userID), paymentorder.StatusIn(OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted), paymentorder.PaidAtGTE(psStartOfDayUTC(time.Now()))).All(ctx)
	if err != nil {
		return err
	}
	var used float64
	for _, order := range orders {
		used += order.Amount
	}
	if used+amount > limit {
		return infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily USD product limit exceeded")
	}
	return nil
}

func validateSquarespaceRetailMetadata(order *dbent.PaymentOrder, metadata map[string]string) error {
	quote := PaymentOrderRetailQuote(order)
	snapshot := psOrderProviderSnapshot(order)
	if quote == nil || snapshot == nil || snapshot.MerchantID == "" {
		return fmt.Errorf("Squarespace payment requires a retail quote and pinned website")
	}
	scope, err := frozenSquarespaceOrderScope(order)
	if err != nil {
		return err
	}
	if scope["orderScopeMode"] == provider.SquarespaceScopeFixedProduct {
		if metadata["product_id"] != scope["productId"] {
			return fmt.Errorf("Squarespace payment product mismatch")
		}
	} else {
		if metadata["order_scope_mode"] != scope["orderScopeMode"] || metadata["payment_purpose"] != scope["paymentPurpose"] || metadata["expected_service_name"] != scope["expectedServiceName"] {
			return fmt.Errorf("Squarespace dedicated payment scope mismatch")
		}
		if _, err := canonicalSquarespaceProductID(metadata["actual_product_id"]); err != nil {
			return err
		}
	}
	if metadata["website_id"] != snapshot.MerchantID || metadata["currency"] != "GBP" || quote.Currency != "GBP" || metadata["checkout_reference"] != quote.CheckoutReference {
		return fmt.Errorf("Squarespace payment website, currency or checkout reference mismatch")
	}
	return ValidateSquarespaceRetailPaymentTimes(quote, metadata)
}

func PaymentOrderClaimMode(order *dbent.PaymentOrder) string {
	if quote := PaymentOrderRetailQuote(order); quote != nil {
		return quote.PaymentClaimMode
	}
	return ""
}

// ValidateSquarespaceRetailPaymentTimes checks every positive payment time, so
// an old partial payment cannot be combined with a newer one to reuse a quote.
func ValidateSquarespaceRetailPaymentTimes(quote *RetailQuote, metadata map[string]string) error {
	if quote == nil {
		return fmt.Errorf("missing retail quote")
	}
	times := []string{metadata["squarespace_paid_on"]}
	if encoded := metadata["upstream_paid_on"]; encoded != "" {
		var all []string
		if json.Unmarshal([]byte(encoded), &all) != nil || len(all) == 0 || len(all) > 100 {
			return fmt.Errorf("invalid upstream payment times")
		}
		times = append(times, all...)
	}
	for _, timestamp := range times {
		paidAt, err := time.Parse(time.RFC3339, timestamp)
		if err != nil || paidAt.Before(quote.IssuedAt) || paidAt.After(quote.ExpiresAt) || paidAt.After(time.Now().Add(time.Minute)) {
			return fmt.Errorf("Squarespace payment was not made within the quoted price validity window")
		}
	}
	return nil
}

func PaymentOrderSquarespaceProductID(order *dbent.PaymentOrder) string {
	if order == nil {
		return ""
	}
	product, _ := canonicalSquarespaceProductID(psSnapshotStringValue(order.ProviderSnapshot["product_id"]))
	return product
}

// Historical queries and refunds must retain the merchant/product originally
// quoted, even if the active provider is later repointed to another Pay Link.
func (s *PaymentService) createHistoricalSquarespaceProvider(ctx context.Context, instance *dbent.PaymentProviderInstance, order *dbent.PaymentOrder) (payment.Provider, error) {
	snapshot := psOrderProviderSnapshot(order)
	scope, scopeErr := frozenSquarespaceOrderScope(order)
	if snapshot == nil || snapshot.MerchantID == "" || scopeErr != nil {
		return nil, fmt.Errorf("Squarespace order has no frozen merchant and scope")
	}
	config, err := s.loadBalancer.GetInstanceConfig(ctx, instance.ID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		config = map[string]string{}
	}
	config["websiteId"] = snapshot.MerchantID
	for key, value := range scope {
		config[key] = value
	}
	config["currency"] = "GBP"
	if original := psSnapshotStringValue(order.ProviderSnapshot["reference_field_label"]); original != "" {
		config["referenceFieldLabel"] = original
	}
	if original := psSnapshotStringValue(order.ProviderSnapshot["pay_link_url"]); original != "" {
		config["payLinkUrl"] = original
	}
	if original := psSnapshotStringValue(order.ProviderSnapshot["payment_claim_mode"]); original != "" {
		config["paymentClaimMode"] = original
	}
	return s.createSquarespaceQueryProvider(ctx, instance, config)
}

var squarespaceRetailProductIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func canonicalSquarespaceProductID(raw string) (string, error) {
	canonical := strings.ToLower(strings.TrimSpace(raw))
	if !squarespaceRetailProductIDPattern.MatchString(canonical) {
		return "", fmt.Errorf("Pay Link product ID must be 24 hexadecimal characters")
	}
	return canonical, nil
}
