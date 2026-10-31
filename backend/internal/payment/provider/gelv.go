package provider

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

// Gelv uses existing shared products for mini-program checkout and arbitrary
// cash for Alipay hosted checkout. Only authenticated site partner orders are eligible
// for site-credit fulfillment.
// It has no webhook, cancellation or partner refund API in the supplied v1.
const GelvAPIBase = "https://alar1c.cn"

// This temporary ceiling applies only when creating Alipay payments. Historical
// queries and fulfillment continue to use their original frozen amounts.
const GelvAlipayMaxNewAmountMinor int64 = 5000

func GelvAlipayNewAmountMinor(amount string) (int64, error) {
	return gelvAlipayAmountMinor(amount, GelvAlipayMaxNewAmountMinor)
}

func gelvAlipayAmountMinor(amount string, maxMinor int64) (int64, error) {
	value, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || value.LessThan(decimal.NewFromInt(1).Shift(-2)) || value.GreaterThan(decimal.NewFromInt(maxMinor).Shift(-2)) {
		return 0, gelvError("invalid_alipay_amount")
	}
	minor, err := payment.AmountToMinorUnit(amount, "CNY")
	if err != nil {
		return 0, gelvError("invalid_alipay_amount")
	}
	return minor, nil
}

var (
	ErrGelvUnsupported          = errors.New("gelv: operation unsupported; use channel manual refund")
	gelvCheckoutFragmentPattern = regexp.MustCompile(`^(id=[a-f0-9]{24}&token=[a-f0-9]{64}|token=[a-f0-9]{64}&id=[a-f0-9]{24})$`)
	gelvFeeBPSPattern           = regexp.MustCompile(`^[0-9]+$`)
	gelvOrderIDPattern          = regexp.MustCompile(`^[a-f0-9]{24}$`)
	gelvReferencePattern        = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)
	gelvPartnerPattern          = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
	gelvPlatformPaymentPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{6,128}$`)
)

type gelvPaymentRecoveryKey struct{}

// WithGelvPaymentRecovery is for replaying an already-persisted local order.
// Bind the exception to its immutable reference; HTTP request data must never
// enable it. The service checks the new-order cap before persisting any order.
func WithGelvPaymentRecovery(ctx context.Context, reference string) context.Context {
	return context.WithValue(ctx, gelvPaymentRecoveryKey{}, reference)
}

// GelvError intentionally excludes upstream bodies, credentials and signed headers.
type GelvError struct {
	Kind       string
	HTTPStatus int
}

func (e *GelvError) Error() string { return fmt.Sprintf("gelv: %s (http=%d)", e.Kind, e.HTTPStatus) }
func gelvError(kind string) error  { return &GelvError{Kind: kind} }

type Gelv struct {
	partnerID string
	secret    string
	products  map[string]bool
	client    *http.Client
}

func NewGelv(_ string, config map[string]string) (*Gelv, error) {
	id, secret := strings.TrimSpace(config["partnerId"]), config["partnerSecret"]
	if !gelvPartnerPattern.MatchString(id) || len(secret) < 32 || strings.Contains(strings.ToUpper(secret), "REPLACE") || id == "local-topup-test" {
		return nil, gelvError("invalid_partner_credentials")
	}
	products := map[string]bool{}
	for _, raw := range strings.Split(config["productIds"], ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 || products[strconv.FormatInt(id, 10)] {
			return nil, gelvError("invalid_product_allowlist")
		}
		products[strconv.FormatInt(id, 10)] = true
	}
	if len(products) == 0 || len(products) > 100 {
		return nil, gelvError("invalid_product_allowlist")
	}
	// The origin is fixed by the reviewed partner protocol. Config cannot turn
	// this credential-bearing client into an arbitrary URL fetcher.
	return &Gelv{partnerID: id, secret: secret, products: products, client: &http.Client{
		Timeout:       18 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type GelvCreditOption struct {
	CashCNY        float64 `json:"cash_cny"`
	Credits        float64 `json:"credits"`
	AndroidCredits float64 `json:"android_credits"`
	IOSCredits     float64 `json:"ios_credits"`
}

// The user-approved fee schedule is 1% Android / 12% Apple IAP. The lower
// bound identifies a fixed cash tier before the paying device is known.
func GelvCreditOptions(_ map[string]string) ([]GelvCreditOption, error) {
	var out []GelvCreditOption
	for _, cash := range []int64{10, 30, 50, 100, 200, 500} {
		android, _ := GelvCredits(cash*100, "wechat_virtual")
		ios, _ := GelvCredits(cash*100, "apple_iap")
		out = append(out, GelvCreditOption{CashCNY: float64(cash), Credits: ios, AndroidCredits: android, IOSCredits: ios})
	}
	return out, nil
}

// Round down to two site-credit decimals. A future cash price must never
// over-credit due to binary floating point or a fractional minor unit.
func GelvCredits(cashMinor int64, channel string) (float64, error) {
	var factor int64
	switch channel {
	case "wechat_virtual":
		factor = 99
	case "apple_iap":
		factor = 88
	default:
		return 0, gelvError("paid_channel_unverified")
	}
	if cashMinor <= 0 || cashMinor > 50000 {
		return 0, gelvError("invalid_amount")
	}
	return decimal.NewFromInt(cashMinor).Mul(decimal.NewFromInt(factor)).Div(decimal.NewFromInt(10000)).Truncate(2).InexactFloat64(), nil
}

// GelvAlipayFeeBPS reads the explicit partner agreement, independently of any
// platform-reported rate. Missing configuration must never imply a free channel.
func GelvAlipayFeeBPS(config map[string]string) (int64, error) {
	raw := strings.TrimSpace(config["alipayFeeRateBps"])
	if !gelvFeeBPSPattern.MatchString(raw) {
		return 0, gelvError("invalid_alipay_fee_rate_bps")
	}
	bps, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || bps < 0 || bps > 9999 {
		return 0, gelvError("invalid_alipay_fee_rate_bps")
	}
	return bps, nil
}

// GelvAlipayCredits deducts the agreed fee rounded to whole fen before converting
// the net fen to site credits. It does not estimate a platform settlement fee.
func GelvAlipayCredits(cashMinor, bps int64) (float64, error) {
	if cashMinor <= 0 || cashMinor > 50000 {
		return 0, gelvError("invalid_amount")
	}
	if bps < 0 || bps > 9999 {
		return 0, gelvError("invalid_alipay_fee_rate_bps")
	}
	cash := decimal.NewFromInt(cashMinor)
	fee := cash.Mul(decimal.NewFromInt(bps)).Div(decimal.NewFromInt(10000)).Round(0)
	return cash.Sub(fee).Div(decimal.NewFromInt(100)).InexactFloat64(), nil
}

// GelvAlipayCreditOptions quotes the six existing products using only the
// explicit Alipay agreement; legacy Android and iOS credits are not inferred.
func GelvAlipayCreditOptions(config map[string]string) ([]GelvCreditOption, error) {
	bps, err := GelvAlipayFeeBPS(config)
	if err != nil {
		return nil, err
	}
	var out []GelvCreditOption
	for _, cash := range []int64{10, 30, 50, 100, 200, 500} {
		credits, err := GelvAlipayCredits(cash*100, bps)
		if err != nil {
			return nil, err
		}
		out = append(out, GelvCreditOption{CashCNY: float64(cash), Credits: credits})
	}
	return out, nil
}

func (*Gelv) Name() string        { return "WeChat mini-program" }
func (*Gelv) ProviderKey() string { return payment.TypeGelv }
func (*Gelv) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeGelv, payment.TypeGelvAlipay}
}
func (g *Gelv) MerchantIdentityMetadata() map[string]string {
	return map[string]string{"partner_id": g.partnerID, "currency": "CNY"}
}

func GelvAmountMinor(amount string) (int64, error) {
	minor, err := payment.AmountToMinorUnit(amount, "CNY")
	if err != nil {
		return 0, gelvError("invalid_amount")
	}
	switch minor {
	case 1000, 3000, 5000, 10000, 20000, 50000:
		return minor, nil
	}
	return 0, gelvError("unsupported_denomination")
}

func GelvProductForMinor(amount int64) (string, error) {
	product, ok := map[int64]string{1000: "3", 3000: "4", 5000: "5", 10000: "6", 20000: "7", 50000: "8"}[amount]
	if !ok {
		return "", gelvError("unsupported_denomination")
	}
	return product, nil
}

type GelvOrder struct {
	ID                        string `json:"id"`
	PartnerOrderID            string `json:"partnerOrderId"`
	Amount                    int64  `json:"amount"`
	Currency                  string `json:"currency"`
	ProductID                 string `json:"productId"`
	Status                    string `json:"status"`
	Refunded                  *int64 `json:"refunded"`
	WxOrderID                 string `json:"wxOrderId"`
	MerchantOrderID           string `json:"merchantOrderId"`
	PaymentChannel            string `json:"paymentChannel"`
	CheckoutChannel           string `json:"checkoutChannel"`
	AlipayTradeNo             string `json:"alipayTradeNo"`
	RefundPending             bool   `json:"refundPending"`
	ChannelVerified           bool   `json:"channelVerified"`
	ChannelVerificationStatus string `json:"channelVerificationStatus"`
	ChannelVerifiedAt         *int64 `json:"channelVerifiedAt"`
	FeeRateBps                *int64 `json:"feeRateBps"`
	SettlementState           *int64 `json:"settlementState"`
	PlatformFeeFen            *int64 `json:"platformFeeFen"`
	CPSFeeFen                 *int64 `json:"cpsFeeFen"`
	SettledAt                 *int64 `json:"settledAt"`
}

func (g *Gelv) validateOrder(o *GelvOrder) error {
	if !gelvOrderIDPattern.MatchString(o.ID) || !gelvReferencePattern.MatchString(o.PartnerOrderID) || o.Amount <= 0 || o.Amount > 50000 || o.Refunded == nil || *o.Refunded < 0 || *o.Refunded > o.Amount || o.Currency != "CNY" {
		return gelvError("invalid_order_scope_or_amount")
	}
	if o.CheckoutChannel != "alipay_web" {
		// Only the mini-program purchases a denomination-mapped product.
		if !g.products[o.ProductID] {
			return gelvError("invalid_order_scope_or_amount")
		}
		expectedProduct, err := GelvProductForMinor(o.Amount)
		if err != nil {
			return err
		}
		if o.ProductID != expectedProduct {
			return gelvError("product_denomination_mismatch")
		}
	} else if o.ProductID != "" && !g.products[o.ProductID] {
		return gelvError("invalid_order_scope_or_amount")
	}
	switch o.Status {
	case "pending", "paid", "partially_refunded", "refunded", "closed":
	default:
		return gelvError("unknown_order_status")
	}
	// Old records predate checkoutChannel and belong only to the legacy flow.
	if o.CheckoutChannel == "" {
		o.CheckoutChannel = "wechat_virtual"
	}
	switch o.CheckoutChannel {
	case "wechat_virtual":
		if o.PaymentChannel == "alipay_web" {
			return gelvError("checkout_channel_mismatch")
		}
	case "alipay_web":
		if o.PaymentChannel != "" && o.PaymentChannel != "alipay_web" {
			return gelvError("checkout_channel_mismatch")
		}
	default:
		return gelvError("invalid_checkout_channel")
	}
	evidence := o.WxOrderID
	if o.PaymentChannel == "alipay_web" {
		evidence = o.AlipayTradeNo
	}
	if o.Status == "paid" && !o.RefundPending && (!gelvPlatformPaymentPattern.MatchString(evidence) || *o.Refunded != 0) {
		return gelvError("incomplete_paid_evidence")
	}
	if (o.Status == "partially_refunded" && (*o.Refunded <= 0 || *o.Refunded >= o.Amount)) || (o.Status == "refunded" && *o.Refunded != o.Amount) {
		return gelvError("inconsistent_refund_state")
	}
	return nil
}

func (g *Gelv) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if (req.PaymentType != payment.TypeGelv && req.PaymentType != payment.TypeGelvAlipay) || !gelvReferencePattern.MatchString(req.OrderID) {
		return nil, gelvError("invalid_order_reference")
	}
	var minor int64
	var err error
	if req.PaymentType == payment.TypeGelvAlipay {
		if reference, _ := ctx.Value(gelvPaymentRecoveryKey{}).(string); reference != "" && reference == req.OrderID {
			// Preserve the prior 500-CNY ceiling for idempotent recovery only.
			minor, err = gelvAlipayAmountMinor(req.Amount, 50000)
		} else {
			minor, err = GelvAlipayNewAmountMinor(req.Amount)
		}
	} else {
		minor, err = GelvAmountMinor(req.Amount)
	}
	if err != nil {
		return nil, err
	}
	checkoutChannel := "wechat_virtual"
	requestChannel := ""
	if req.PaymentType == payment.TypeGelvAlipay {
		checkoutChannel, requestChannel = "alipay_web", "alipay_web"
	}
	var out struct {
		Order       GelvOrder `json:"order"`
		Image       string    `json:"image"`
		CheckoutURL string    `json:"checkoutUrl"`
	}
	if err = g.post(ctx, "/api/partner/v1/orders", struct {
		PartnerOrderID string `json:"partnerOrderId"`
		Amount         int64  `json:"amount"`
		PaymentChannel string `json:"paymentChannel,omitempty"`
	}{req.OrderID, minor, requestChannel}, &out); err != nil {
		return nil, err
	}
	if err = g.validateOrder(&out.Order); err != nil {
		return nil, err
	}
	if out.Order.PartnerOrderID != req.OrderID || out.Order.Amount != minor {
		return nil, gelvError("created_order_mismatch")
	}
	if out.Order.CheckoutChannel != checkoutChannel {
		return nil, gelvError("checkout_channel_mismatch")
	}
	if req.PaymentType == payment.TypeGelvAlipay {
		if !validGelvCheckoutURL(out.CheckoutURL, out.Order.ID) {
			return nil, gelvError("invalid_alipay_checkout_url")
		}
		return &payment.CreatePaymentResponse{TradeNo: out.Order.ID, PayURL: out.CheckoutURL, Currency: "CNY", Metadata: g.orderMetadata(&out.Order)}, nil
	}
	if out.CheckoutURL != "" {
		return nil, gelvError("checkout_channel_mismatch")
	}
	if out.Image != "" && !validGelvImage(out.Image) {
		return nil, gelvError("invalid_miniprogram_image")
	}
	// An idempotent replay can legitimately omit the image (already bound,
	// paid or expired). The service must retain an earlier persisted image.
	return &payment.CreatePaymentResponse{TradeNo: out.Order.ID, QRCode: out.Image, Currency: "CNY", Metadata: g.orderMetadata(&out.Order)}, nil
}

func (g *Gelv) QueryOrder(ctx context.Context, id string) (*payment.QueryOrderResponse, error) {
	if !gelvOrderIDPattern.MatchString(id) {
		return nil, gelvError("invalid_order_id")
	}
	var out struct {
		Order GelvOrder `json:"order"`
	}
	if err := g.post(ctx, "/api/partner/v1/order", struct {
		ID string `json:"id"`
	}{id}, &out); err != nil {
		return nil, err
	}
	o := &out.Order
	if err := g.validateOrder(o); err != nil {
		return nil, err
	}
	if o.ID != id {
		return nil, gelvError("queried_order_mismatch")
	}
	if o.Status == "paid" && !o.RefundPending && o.ChannelVerificationStatus == "conflict" {
		return nil, gelvError("paid_channel_conflict")
	}
	if o.Status == "paid" && !o.RefundPending && (!o.ChannelVerified || o.ChannelVerificationStatus != "verified" || (o.PaymentChannel != "wechat_virtual" && o.PaymentChannel != "apple_iap" && o.PaymentChannel != "alipay_web")) {
		return nil, gelvError("paid_channel_unverified")
	}
	if o.Status == "paid" && !o.RefundPending && o.PaymentChannel != "alipay_web" {
		expectedRate := int64(100)
		if o.PaymentChannel == "apple_iap" {
			expectedRate = 1200
		}
		if o.FeeRateBps == nil || *o.FeeRateBps != expectedRate {
			return nil, gelvError("paid_fee_rule_mismatch")
		}
	}
	status := payment.ProviderStatusPending
	switch o.Status {
	case "paid":
		status = payment.ProviderStatusPaid
	case "partially_refunded", "refunded":
		status = payment.ProviderStatusRefunded
	case "closed":
		status = payment.ProviderStatusFailed
	}
	if o.RefundPending {
		status = payment.ProviderStatusPending
	}
	return &payment.QueryOrderResponse{TradeNo: o.ID, Status: status, Amount: float64(o.Amount) / 100, Currency: "CNY", Metadata: g.orderMetadata(o)}, nil
}

// orderMetadata keeps unknown numeric platform evidence absent, distinct from
// a reported zero. Agreement rates are never substituted for these values.
func (g *Gelv) orderMetadata(o *GelvOrder) map[string]string {
	metadata := map[string]string{
		"partner_id": g.partnerID, "partner_order_id": o.PartnerOrderID, "gelv_order_id": o.ID,
		"product_id": o.ProductID, "currency": "CNY", "wx_order_id": o.WxOrderID,
		"merchant_order_id": o.MerchantOrderID,
		"gelv_status":       o.Status, "amount_minor": strconv.FormatInt(o.Amount, 10), "refunded_minor": strconv.FormatInt(*o.Refunded, 10),
		"checkout_channel": o.CheckoutChannel, "payment_channel": o.PaymentChannel,
		"alipay_trade_no": o.AlipayTradeNo, "refund_pending": strconv.FormatBool(o.RefundPending),
		"channel_verified": strconv.FormatBool(o.ChannelVerified), "channel_verification_status": o.ChannelVerificationStatus,
	}
	if o.RefundPending {
		metadata["reconciliation_hold"] = "refund_pending"
	}
	for key, value := range map[string]*int64{"fee_rate_bps": o.FeeRateBps, "channel_verified_at": o.ChannelVerifiedAt, "settlement_state": o.SettlementState, "platform_fee_fen": o.PlatformFeeFen, "cps_fee_fen": o.CPSFeeFen, "settled_at": o.SettledAt} {
		if value != nil {
			metadata[key] = strconv.FormatInt(*value, 10)
		}
	}
	return metadata
}

// Validate the original URL as well as its parsed components. The raw fragment
// grammar disallows duplicate keys, escaped aliases and extra parameters.
func validGelvCheckoutURL(raw, id string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "alar1c.cn" || u.User != nil || u.Opaque != "" {
		return false
	}
	if u.Path != "/partner-pay/" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery {
		return false
	}
	if u.RawFragment != "" || !strings.HasPrefix(raw, GelvAPIBase+"/partner-pay/#") || !gelvCheckoutFragmentPattern.MatchString(u.Fragment) {
		return false
	}
	for _, pair := range strings.Split(u.Fragment, "&") {
		key, value, _ := strings.Cut(pair, "=")
		if key == "id" {
			return value == id
		}
	}
	return false
}

func (*Gelv) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, ErrGelvUnsupported
}
func (*Gelv) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, ErrGelvUnsupported
}

func validGelvImage(raw string) bool {
	if len(raw) > 1<<20 {
		return false
	}
	var encoded string
	switch {
	case strings.HasPrefix(raw, "data:image/png;base64,"):
		encoded = strings.TrimPrefix(raw, "data:image/png;base64,")
	case strings.HasPrefix(raw, "data:image/jpeg;base64,"):
		encoded = strings.TrimPrefix(raw, "data:image/jpeg;base64,")
	default:
		return false
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	if strings.HasPrefix(raw, "data:image/png;") {
		return len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n"
	}
	return len(b) > 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff
}

func (g *Gelv) post(ctx context.Context, route string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return gelvError("invalid_request")
	}
	nonceBytes := make([]byte, 16)
	if _, err = rand.Read(nonceBytes); err != nil {
		return gelvError("nonce_unavailable")
	}
	nonce, timestamp := hex.EncodeToString(nonceBytes), strconv.FormatInt(time.Now().UnixMilli(), 10)
	hash := sha256.Sum256(raw)
	mac := hmac.New(sha256.New, []byte(g.secret))
	_, _ = mac.Write([]byte(strings.Join([]string{"POST", route, timestamp, nonce, hex.EncodeToString(hash[:])}, "\n")))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GelvAPIBase+route, strings.NewReader(string(raw)))
	if err != nil {
		return gelvError("invalid_request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Sub2API-Gelv-Partner/1")
	req.Header.Set("X-Gelv-Partner", g.partnerID)
	req.Header.Set("X-Gelv-Timestamp", timestamp)
	req.Header.Set("X-Gelv-Nonce", nonce)
	req.Header.Set("X-Gelv-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := g.client.Do(req)
	if err != nil {
		return gelvError("transport_result_unknown")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return &GelvError{Kind: "upstream_rejected", HTTPStatus: resp.StatusCode}
	}
	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(rawResponse) > 2<<20 {
		return gelvError("invalid_response_size")
	}
	if json.Unmarshal(rawResponse, out) != nil {
		return gelvError("invalid_response")
	}
	return nil
}
