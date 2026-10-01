package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/shopspring/decimal"
)

const (
	SquarespaceReferenceFieldLabel       = "RynexAI top-up reference"
	SquarespacePaymentGateway            = "SQSP_PAYMENTS"
	SquarespaceScopeFixedProduct         = "fixed_product"
	SquarespaceScopeDedicatedSiteService = "dedicated_site_service"
	SquarespaceBalanceTopupPurpose       = "balance_topup_only"
	SquarespaceExpectedServiceName       = "Pay"
	squarespaceAPIBase                   = "https://api.squarespace.com"
	squarespaceUserAgent                 = "RynexAI-Sub2API-Squarespace/1.0"
	squarespaceMaxResponseBytes          = 8 << 20
	squarespaceDefaultMaxPages           = 20
)

var (
	ErrSquarespaceUnsupported   = errors.New("squarespace: operation unsupported")
	squarespaceIDPattern        = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)
	squarespaceProductIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)
	squarespaceRefPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
	squarespaceMoneyPattern     = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]{1,2})?$`)
	squarespaceJSONMoneyPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,2})?$`)
)

// SquarespaceAPIError contains no upstream body, URL, token, or customer fields.
type SquarespaceAPIError struct {
	Kind       string
	HTTPStatus int
	RetryAfter time.Duration
}

func (e *SquarespaceAPIError) Error() string {
	return fmt.Sprintf("squarespace: %s (http=%d)", e.Kind, e.HTTPStatus)
}

func squarespaceError(kind string) error { return &SquarespaceAPIError{Kind: kind} }

// SquarespaceTokenSource owns refresh and durable encrypted pair storage outside
// the provider. It must not return a newly refreshed token before persistence.
type SquarespaceTokenSource interface {
	AccessToken(context.Context) (string, error)
}

type SquarespaceTokenSourceFunc func(context.Context) (string, error)

func (f SquarespaceTokenSourceFunc) AccessToken(ctx context.Context) (string, error) { return f(ctx) }

type SquarespaceMoney struct {
	Currency string `json:"currency"`
	Value    string `json:"value"`
}

// UnmarshalJSON accepts the official string and numeric representations without
// ever decoding through float64. Fractional minor units remain unsupported.
func (m *SquarespaceMoney) UnmarshalJSON(data []byte) error {
	var raw struct {
		Currency string          `json:"currency"`
		Value    json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return squarespaceError("invalid_money")
	}
	if len(raw.Value) == 0 || len(raw.Value) > 128 {
		return squarespaceError("invalid_money")
	}
	m.Currency = raw.Currency
	if raw.Value[0] == '"' {
		if err := json.Unmarshal(raw.Value, &m.Value); err != nil {
			return squarespaceError("invalid_money")
		}
		return nil
	}
	value := string(raw.Value)
	if !squarespaceJSONMoneyPattern.MatchString(value) {
		return squarespaceError("invalid_money")
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || !parsed.Shift(2).Equal(parsed.Shift(2).Truncate(0)) {
		return squarespaceError("fractional_minor_units")
	}
	m.Value = parsed.StringFixed(2)
	return nil
}

// MinorUnits parses exact signed two-decimal money. Charged/refund/fee amounts
// require explicit nonnegative guards; final net settlement may be negative.
func (m SquarespaceMoney) MinorUnits(expectedCurrency string) (int64, error) {
	if m.Currency != expectedCurrency || len(m.Value) > 128 || !squarespaceMoneyPattern.MatchString(m.Value) {
		return 0, squarespaceError("invalid_money_or_currency")
	}
	parsed, err := decimal.NewFromString(m.Value)
	if err != nil {
		return 0, squarespaceError("invalid_money")
	}
	minor := parsed.Shift(2)
	if minor.LessThan(decimal.NewFromInt(math.MinInt64)) || minor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, squarespaceError("money_overflow")
	}
	return minor.IntPart(), nil
}

type SquarespaceOrder struct {
	ID              string                `json:"id"`
	OrderNumber     string                `json:"orderNumber"`
	CustomerEmail   string                `json:"-"` // receipt-claim memory only; never log or persist
	PaymentState    string                `json:"paymentState"`
	TestMode        *bool                 `json:"testmode"`
	GrandTotal      SquarespaceMoney      `json:"grandTotal"`
	Subtotal        SquarespaceMoney      `json:"subtotal"`
	ShippingTotal   SquarespaceMoney      `json:"shippingTotal"`
	TaxTotal        SquarespaceMoney      `json:"taxTotal"`
	DiscountTotal   SquarespaceMoney      `json:"discountTotal"`
	RefundedTotal   SquarespaceMoney      `json:"refundedTotal"`
	CreatedOn       string                `json:"createdOn"`
	ModifiedOn      string                `json:"modifiedOn"`
	TopUpReference  string                `json:"-"`
	ReferenceStatus string                `json:"-"` // valid/missing/duplicate/invalid
	LineItems       []SquarespaceLineItem `json:"lineItems"`
}

// Only non-customer item identifiers are decoded. Product names, descriptions,
// customizations, and addresses are intentionally excluded from this model.
type SquarespaceLineItem struct {
	ID                  string           `json:"id"`
	ProductID           string           `json:"productId"`
	VariantID           string           `json:"variantId"`
	LineItemType        string           `json:"lineItemType"`
	ProductName         string           `json:"-"`
	Quantity            *int             `json:"quantity"`
	UnitPricePaid       SquarespaceMoney `json:"unitPricePaid"`
	VariantIsNull       bool             `json:"-"`
	SKUIsNull           bool             `json:"-"`
	CustomizationsEmpty bool             `json:"-"`
}

func (SquarespaceLineItem) String() string   { return "SquarespaceLineItem{free text redacted}" }
func (SquarespaceLineItem) GoString() string { return "SquarespaceLineItem{free text redacted}" }

func (item *SquarespaceLineItem) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ID             string           `json:"id"`
		ProductID      string           `json:"productId"`
		VariantID      json.RawMessage  `json:"variantId"`
		SKU            json.RawMessage  `json:"sku"`
		LineItemType   string           `json:"lineItemType"`
		ProductName    string           `json:"productName"`
		Quantity       *int             `json:"quantity"`
		UnitPricePaid  SquarespaceMoney `json:"unitPricePaid"`
		Customizations json.RawMessage  `json:"customizations"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return squarespaceError("invalid_line_item")
	}
	result := SquarespaceLineItem{ID: wire.ID, ProductID: wire.ProductID, LineItemType: wire.LineItemType,
		ProductName: wire.ProductName, Quantity: wire.Quantity, UnitPricePaid: wire.UnitPricePaid}
	result.VariantIsNull = string(wire.VariantID) == "null"
	if len(wire.VariantID) > 0 && !result.VariantIsNull {
		if json.Unmarshal(wire.VariantID, &result.VariantID) != nil {
			return squarespaceError("invalid_variant_id")
		}
	}
	result.SKUIsNull = string(wire.SKU) == "null"
	// Customization values can contain customer data; inspect only emptiness,
	// never decode or retain the contents.
	if string(wire.Customizations) == "null" {
		// The live Pay service order explicitly represents no customizations as
		// null. Presence is required; a missing field is not inferred as empty.
		result.CustomizationsEmpty = true
	} else if len(wire.Customizations) > 0 {
		var values []json.RawMessage
		if json.Unmarshal(wire.Customizations, &values) == nil && len(values) == 0 {
			result.CustomizationsEmpty = true
		}
	}
	*item = result
	return nil
}

func squarespaceOrderScopeConfig(cfg map[string]string) (map[string]string, error) {
	mode := strings.TrimSpace(cfg["orderScopeMode"])
	if mode == "" {
		mode = SquarespaceScopeFixedProduct
	}
	result := map[string]string{"orderScopeMode": mode}
	switch mode {
	case SquarespaceScopeFixedProduct:
		pid := strings.ToLower(strings.TrimSpace(cfg["productId"]))
		if !squarespaceProductIDPattern.MatchString(pid) {
			return nil, squarespaceError("invalid_topup_product_id")
		}
		result["productId"] = pid
	case SquarespaceScopeDedicatedSiteService:
		if strings.TrimSpace(cfg["paymentPurpose"]) != SquarespaceBalanceTopupPurpose || strings.TrimSpace(cfg["expectedServiceName"]) != SquarespaceExpectedServiceName {
			return nil, squarespaceError("invalid_dedicated_site_scope")
		}
		result["paymentPurpose"] = SquarespaceBalanceTopupPurpose
		result["expectedServiceName"] = SquarespaceExpectedServiceName
	default:
		return nil, squarespaceError("unsupported_order_scope_mode")
	}
	return result, nil
}

// ValidateSquarespaceOrderScope validates the explicit merchant business scope.
// Dedicated-site authority is a user-approved merchant boundary, not proof that
// a generic channel=web order originated from a particular Pay Link URL.
func ValidateSquarespaceOrderScope(order *SquarespaceOrder, cfg map[string]string) error {
	scope, err := squarespaceOrderScopeConfig(cfg)
	if err != nil {
		return err
	}
	if scope["orderScopeMode"] == SquarespaceScopeFixedProduct {
		return ValidateSquarespaceTopupProduct(order, scope["productId"])
	}
	if order == nil || len(order.LineItems) != 1 {
		return squarespaceError("dedicated_service_cart_invalid")
	}
	item := order.LineItems[0]
	if !squarespaceIDPattern.MatchString(item.ID) || !squarespaceProductIDPattern.MatchString(strings.ToLower(item.ProductID)) ||
		item.LineItemType != "SERVICE" || item.ProductName != SquarespaceExpectedServiceName || item.Quantity == nil || *item.Quantity != 1 ||
		!item.VariantIsNull || !item.SKUIsNull || !item.CustomizationsEmpty {
		return squarespaceError("dedicated_service_item_invalid")
	}
	grand, err := order.GrandTotal.MinorUnits("GBP")
	if err != nil || grand <= 0 {
		return squarespaceError("dedicated_service_grand_invalid")
	}
	subtotal, err := order.Subtotal.MinorUnits("GBP")
	if err != nil || subtotal != grand {
		return squarespaceError("dedicated_service_subtotal_invalid")
	}
	unit, err := item.UnitPricePaid.MinorUnits("GBP")
	if err != nil || unit != grand {
		return squarespaceError("dedicated_service_unit_price_invalid")
	}
	for _, component := range []SquarespaceMoney{order.ShippingTotal, order.TaxTotal, order.DiscountTotal} {
		value, err := component.MinorUnits("GBP")
		if err != nil || value != 0 {
			return squarespaceError("dedicated_service_component_nonzero_or_missing")
		}
	}
	return nil
}

// ValidateSquarespaceTopupProduct is a business-purpose check, separate from
// pure financial evidence. Call it using the immutable quote product snapshot
// when binding a receipt or validating a historical refund.
func ValidateSquarespaceTopupProduct(order *SquarespaceOrder, expectedProductID string) error {
	expectedProductID = strings.ToLower(strings.TrimSpace(expectedProductID))
	if !squarespaceProductIDPattern.MatchString(expectedProductID) {
		return squarespaceError("invalid_topup_product_id")
	}
	if order == nil || len(order.LineItems) != 1 || strings.ToLower(order.LineItems[0].ProductID) != expectedProductID || !squarespaceProductIDPattern.MatchString(strings.ToLower(order.LineItems[0].ProductID)) || !squarespaceIDPattern.MatchString(order.LineItems[0].ID) {
		return squarespaceError("topup_product_mismatch")
	}
	if order.LineItems[0].VariantID != "" && !squarespaceIDPattern.MatchString(order.LineItems[0].VariantID) {
		return squarespaceError("invalid_topup_variant_id")
	}
	return nil
}

func (SquarespaceOrder) String() string   { return "SquarespaceOrder{customer data redacted}" }
func (SquarespaceOrder) GoString() string { return "SquarespaceOrder{customer data redacted}" }

type SquarespacePagination struct {
	HasNextPage    bool   `json:"hasNextPage"`
	NextPageCursor string `json:"nextPageCursor"`
}

type SquarespaceOrderPage struct {
	Orders     []SquarespaceOrder    `json:"result"`
	Pagination SquarespacePagination `json:"pagination"`
}

type SquarespaceTransactionPage struct {
	Documents  []SquarespaceTransactionDocument `json:"documents"`
	Pagination SquarespacePagination            `json:"pagination"`
}

type SquarespaceTransactionDocument struct {
	ID                  string               `json:"id"`
	SalesOrderID        string               `json:"salesOrderId"`
	Voided              *bool                `json:"voided"`
	PaymentGatewayError string               `json:"paymentGatewayError"`
	Total               SquarespaceMoney     `json:"total"`
	TotalNetPayment     SquarespaceMoney     `json:"totalNetPayment"`
	Payments            []SquarespacePayment `json:"payments"`
}

type SquarespacePayment struct {
	ID                    string                     `json:"id"`
	ExternalTransactionID string                     `json:"externalTransactionId"`
	Provider              string                     `json:"provider"`
	PaidOn                string                     `json:"paidOn"`
	Amount                SquarespaceMoney           `json:"amount"`
	NetAmount             SquarespaceMoney           `json:"netAmount"`
	RefundedAmount        SquarespaceMoney           `json:"refundedAmount"`
	ProcessingFees        []SquarespaceProcessingFee `json:"processingFees"`
	Refunds               []SquarespaceRefund        `json:"refunds"`
}

type SquarespaceProcessingFee struct {
	Amount         SquarespaceMoney    `json:"amount"`
	NetAmount      SquarespaceMoney    `json:"netAmount"`
	RefundedAmount SquarespaceMoney    `json:"refundedAmount"`
	FeeRefunds     []SquarespaceRefund `json:"feeRefunds"`
}

type SquarespaceRefund struct {
	ID         string           `json:"id"`
	RefundedOn string           `json:"refundedOn"`
	Amount     SquarespaceMoney `json:"amount"`
}

type squarespaceRawOrder struct {
	SquarespaceOrder
	CustomerEmail  string `json:"customerEmail"`
	FormSubmission []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	} `json:"formSubmission"`
}

func (o squarespaceRawOrder) publicOrder(label string) SquarespaceOrder {
	result := o.SquarespaceOrder
	result.ReferenceStatus = "missing"
	count := 0
	for _, item := range o.FormSubmission {
		if item.Label != label {
			continue
		}
		count++
		candidate := strings.TrimSpace(item.Value)
		if squarespaceRefPattern.MatchString(candidate) {
			result.TopUpReference, result.ReferenceStatus = candidate, "valid"
		} else {
			result.TopUpReference, result.ReferenceStatus = "", "invalid"
		}
	}
	if count > 1 {
		result.TopUpReference, result.ReferenceStatus = "", "duplicate"
	}
	return result
}

type squarespaceRawPagination struct {
	HasNextPage    *bool  `json:"hasNextPage"`
	NextPageCursor string `json:"nextPageCursor"`
	NextPageURL    string `json:"nextPageUrl"`
}

func (p squarespaceRawPagination) validated() (SquarespacePagination, error) {
	if p.HasNextPage == nil || (*p.HasNextPage && !validSquarespaceCursor(p.NextPageCursor)) {
		return SquarespacePagination{}, squarespaceError("invalid_pagination")
	}
	if !*p.HasNextPage && (p.NextPageCursor != "" || p.NextPageURL != "") {
		return SquarespacePagination{}, squarespaceError("inconsistent_terminal_pagination")
	}
	return SquarespacePagination{HasNextPage: *p.HasNextPage, NextPageCursor: p.NextPageCursor}, nil
}

type SquarespaceClient struct {
	websiteID, referenceLabel string
	tokens                    SquarespaceTokenSource
	httpClient                *http.Client
	identityMu                sync.Mutex
	verifiedTokenHash         [32]byte
	identityVerified          bool
}

func NewSquarespaceClient(websiteID, referenceLabel string, tokens SquarespaceTokenSource) (*SquarespaceClient, error) {
	if !squarespaceIDPattern.MatchString(websiteID) {
		return nil, squarespaceError("invalid_website_id")
	}
	if referenceLabel == "" {
		referenceLabel = SquarespaceReferenceFieldLabel
	}
	if len(referenceLabel) > 160 || strings.ContainsAny(referenceLabel, "\r\n\x00") {
		return nil, squarespaceError("invalid_reference_label")
	}
	return &SquarespaceClient{websiteID: websiteID, referenceLabel: referenceLabel, tokens: tokens,
		httpClient: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *SquarespaceClient) WebsiteID() string { return c.websiteID }

func validSquarespaceCursor(value string) bool {
	if value == "" || len(value) > 8192 {
		return false
	}
	for _, char := range value {
		if char < 33 || char > 126 {
			return false
		}
	}
	return true
}

func validSquarespaceToken(value string) bool {
	if value == "" || len(value) > 16384 {
		return false
	}
	for _, char := range value {
		if char < 33 || char > 126 {
			return false
		}
	}
	return true
}

func (c *SquarespaceClient) verifiedToken(ctx context.Context) (string, error) {
	if c.tokens == nil {
		return "", squarespaceError("oauth_unconfigured")
	}
	token, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return "", squarespaceError("oauth_token_unavailable")
	}
	if !validSquarespaceToken(token) {
		return "", squarespaceError("invalid_oauth_token")
	}
	hash := sha256.Sum256([]byte(token))
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if c.identityVerified && hash == c.verifiedTokenHash {
		return token, nil
	}
	var website struct {
		ID string `json:"id"`
	}
	if err := c.fetchJSON(ctx, token, "/1.0/authorization/website", nil, &website); err != nil {
		return "", err
	}
	if website.ID != c.websiteID {
		return "", squarespaceError("website_mismatch")
	}
	c.identityVerified, c.verifiedTokenHash = true, hash
	return token, nil
}

func (c *SquarespaceClient) VerifyWebsite(ctx context.Context) error {
	_, err := c.verifiedToken(ctx)
	return err
}

func (c *SquarespaceClient) fetchJSON(ctx context.Context, token, path string, query url.Values, out any) error {
	endpoint := squarespaceAPIBase + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return squarespaceError("invalid_request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", squarespaceUserAgent)
	req.Header.Set("Accept", "application/json")
	// Even a test/custom transport cannot override the no-redirect policy.
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return squarespaceError("transport_failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		kind := "upstream_rejected"
		if res.StatusCode == 429 {
			kind = "rate_limited"
		}
		if res.StatusCode == 401 || res.StatusCode == 403 {
			kind = "authorization_rejected"
		}
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			kind = "redirect_rejected"
		}
		return &SquarespaceAPIError{Kind: kind, HTTPStatus: res.StatusCode, RetryAfter: squarespaceRetryAfter(res.Header.Get("Retry-After"))}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, squarespaceMaxResponseBytes+1))
	if err != nil {
		return squarespaceError("response_read_failed")
	}
	if len(body) > squarespaceMaxResponseBytes {
		return squarespaceError("response_too_large")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return squarespaceError("invalid_response")
	}
	return nil
}

func squarespaceRetryAfter(raw string) time.Duration {
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 {
		if seconds > 3600 {
			seconds = 3600
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil {
		delay := time.Until(at)
		if delay < 0 {
			return 0
		}
		if delay > time.Hour {
			return time.Hour
		}
		return delay
	}
	return time.Minute
}

func (c *SquarespaceClient) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	token, err := c.verifiedToken(ctx)
	if err != nil {
		return err
	}
	return c.fetchJSON(ctx, token, path, query, out)
}

func (c *SquarespaceClient) ListOrders(ctx context.Context, cursor, modifiedAfter, modifiedBefore string) (*SquarespaceOrderPage, error) {
	query := make(url.Values)
	if cursor != "" {
		if !validSquarespaceCursor(cursor) || modifiedAfter != "" || modifiedBefore != "" {
			return nil, squarespaceError("invalid_order_cursor_parameters")
		}
		query.Set("cursor", cursor)
	} else if modifiedAfter != "" || modifiedBefore != "" {
		after, errA := time.Parse(time.RFC3339Nano, modifiedAfter)
		before, errB := time.Parse(time.RFC3339Nano, modifiedBefore)
		if errA != nil || errB != nil || !after.Before(before) {
			return nil, squarespaceError("invalid_order_window")
		}
		query.Set("modifiedAfter", after.UTC().Format(time.RFC3339Nano))
		query.Set("modifiedBefore", before.UTC().Format(time.RFC3339Nano))
	}
	var raw struct {
		Result     []squarespaceRawOrder    `json:"result"`
		Pagination squarespaceRawPagination `json:"pagination"`
	}
	if err := c.getJSON(ctx, "/1.0/commerce/orders", query, &raw); err != nil {
		return nil, err
	}
	if raw.Result == nil || len(raw.Result) > 50 {
		return nil, squarespaceError("invalid_order_page")
	}
	pagination, err := raw.Pagination.validated()
	if err != nil {
		return nil, err
	}
	page := &SquarespaceOrderPage{Pagination: pagination, Orders: make([]SquarespaceOrder, 0, len(raw.Result))}
	for _, order := range raw.Result {
		if !squarespaceIDPattern.MatchString(order.ID) {
			return nil, squarespaceError("invalid_upstream_order_id")
		}
		page.Orders = append(page.Orders, order.publicOrder(c.referenceLabel))
	}
	return page, nil
}

func (c *SquarespaceClient) GetOrder(ctx context.Context, orderID string) (*SquarespaceOrder, error) {
	return c.getOrder(ctx, orderID, false)
}

// GetOrderForReceiptClaim includes upstream customerEmail in memory only for
// authenticated-email plus purpose-bound OTP verification by the claim service.
func (c *SquarespaceClient) GetOrderForReceiptClaim(ctx context.Context, orderID string) (*SquarespaceOrder, error) {
	return c.getOrder(ctx, orderID, true)
}

func (c *SquarespaceClient) getOrder(ctx context.Context, orderID string, includeClaimEmail bool) (*SquarespaceOrder, error) {
	if !squarespaceIDPattern.MatchString(orderID) {
		return nil, squarespaceError("invalid_upstream_order_id")
	}
	var raw squarespaceRawOrder
	if err := c.getJSON(ctx, "/1.0/commerce/orders/"+url.PathEscape(orderID), nil, &raw); err != nil {
		return nil, err
	}
	if raw.ID != orderID {
		return nil, squarespaceError("upstream_order_mismatch")
	}
	order := raw.publicOrder(c.referenceLabel)
	if includeClaimEmail {
		order.CustomerEmail = raw.CustomerEmail
	}
	return &order, nil
}

// CanonicalSquarespaceReceiptNumber matches UI zero-padding to the official
// sequential orderNumber without using email, amount, or customer information.
func CanonicalSquarespaceReceiptNumber(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) == 0 || len(value) > 20 {
		return "", squarespaceError("invalid_receipt_number")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return "", squarespaceError("invalid_receipt_number")
		}
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "", squarespaceError("invalid_receipt_number")
	}
	return value, nil
}

func (c *SquarespaceClient) FindOrderByNumber(ctx context.Context, receipt, modifiedAfter, modifiedBefore string, maxPages int) (*SquarespaceOrder, error) {
	wanted, err := CanonicalSquarespaceReceiptNumber(receipt)
	if err != nil {
		return nil, err
	}
	if modifiedAfter == "" || modifiedBefore == "" || maxPages <= 0 || maxPages > 100 {
		return nil, squarespaceError("invalid_receipt_lookup_bound")
	}
	var found *SquarespaceOrder
	cursor := ""
	seen := make(map[string]bool)
	for page := 0; page < maxPages; page++ {
		after, before := modifiedAfter, modifiedBefore
		if cursor != "" {
			after, before = "", ""
		}
		result, err := c.ListOrders(ctx, cursor, after, before)
		if err != nil {
			return nil, err
		}
		for _, order := range result.Orders {
			number, err := CanonicalSquarespaceReceiptNumber(order.OrderNumber)
			if err != nil || number != wanted {
				continue
			}
			if found != nil && found.ID != order.ID {
				return nil, squarespaceError("ambiguous_receipt_number")
			}
			copy := order
			found = &copy
		}
		if !result.Pagination.HasNextPage {
			if found == nil {
				return nil, squarespaceError("receipt_not_found")
			}
			return found, nil
		}
		cursor = result.Pagination.NextPageCursor
		if seen[cursor] {
			return nil, squarespaceError("pagination_cycle")
		}
		seen[cursor] = true
	}
	return nil, squarespaceError("receipt_lookup_bound_exceeded")
}

func (c *SquarespaceClient) ListTransactions(ctx context.Context, orderID, cursor string) (*SquarespaceTransactionPage, error) {
	if !squarespaceIDPattern.MatchString(orderID) {
		return nil, squarespaceError("invalid_upstream_order_id")
	}
	query := make(url.Values)
	if cursor != "" {
		if !validSquarespaceCursor(cursor) {
			return nil, squarespaceError("invalid_transaction_cursor")
		}
		query.Set("cursor", cursor)
	} else {
		query.Set("orderId", orderID)
	}
	var raw struct {
		Documents  []SquarespaceTransactionDocument `json:"documents"`
		Pagination squarespaceRawPagination         `json:"pagination"`
	}
	if err := c.getJSON(ctx, "/1.0/commerce/transactions", query, &raw); err != nil {
		return nil, err
	}
	if raw.Documents == nil || len(raw.Documents) > 50 {
		return nil, squarespaceError("invalid_transaction_page")
	}
	for _, document := range raw.Documents {
		if document.SalesOrderID != orderID {
			return nil, squarespaceError("transaction_order_mismatch")
		}
	}
	var pagination SquarespacePagination
	if raw.Pagination.HasNextPage == nil {
		// The live orderId-filtered endpoint returns a nonpaginated Document
		// response. Officially one Document belongs to one order; accept only
		// this constrained first-page shape, never an unbounded missing cursor.
		if cursor != "" || len(raw.Documents) > 1 || raw.Pagination.NextPageCursor != "" || raw.Pagination.NextPageURL != "" {
			return nil, squarespaceError("invalid_transaction_pagination")
		}
	} else {
		var err error
		pagination, err = raw.Pagination.validated()
		if err != nil {
			return nil, err
		}
	}
	return &SquarespaceTransactionPage{Documents: raw.Documents, Pagination: pagination}, nil
}

func (c *SquarespaceClient) ListAllTransactionsForOrder(ctx context.Context, orderID string, maxPages int) ([]SquarespaceTransactionDocument, error) {
	if maxPages <= 0 || maxPages > 100 {
		return nil, squarespaceError("invalid_page_bound")
	}
	var documents []SquarespaceTransactionDocument
	cursor := ""
	seen := make(map[string]bool)
	for page := 0; page < maxPages; page++ {
		result, err := c.ListTransactions(ctx, orderID, cursor)
		if err != nil {
			return nil, err
		}
		documents = append(documents, result.Documents...)
		if !result.Pagination.HasNextPage {
			return documents, nil
		}
		cursor = result.Pagination.NextPageCursor
		if seen[cursor] {
			return nil, squarespaceError("pagination_cycle")
		}
		seen[cursor] = true
	}
	return nil, squarespaceError("pagination_bound_exceeded")
}

type Squarespace struct {
	instanceID string
	config     map[string]string
	client     *SquarespaceClient
}

func NewSquarespace(instanceID string, config map[string]string) (*Squarespace, error) {
	return NewSquarespaceWithTokenSource(instanceID, config, nil)
}

func NewSquarespaceWithTokenSource(instanceID string, config map[string]string, tokens SquarespaceTokenSource) (*Squarespace, error) {
	websiteID := strings.TrimSpace(config["websiteId"])
	scope, err := squarespaceOrderScopeConfig(config)
	if err != nil {
		return nil, err
	}
	payURL, err := url.Parse(strings.TrimSpace(config["payLinkUrl"]))
	if err != nil || payURL.Scheme != "https" || payURL.User != nil || payURL.Port() != "" ||
		!strings.HasSuffix(strings.ToLower(payURL.Hostname()), ".squarespace.com") ||
		!strings.HasPrefix(payURL.Path, "/pay-link/") || payURL.Fragment != "" {
		return nil, squarespaceError("invalid_pay_link_url")
	}
	currency := strings.TrimSpace(config["currency"])
	if currency == "" {
		currency = "GBP"
	}
	if currency != "GBP" {
		return nil, squarespaceError("unsupported_currency")
	}
	label := config["referenceFieldLabel"]
	if label == "" {
		label = SquarespaceReferenceFieldLabel
	}
	client, err := NewSquarespaceClient(websiteID, label, tokens)
	if err != nil {
		return nil, err
	}
	cfg := map[string]string{"websiteId": websiteID, "payLinkUrl": payURL.String(), "currency": "GBP", "referenceFieldLabel": label}
	for key, value := range scope {
		cfg[key] = value
	}
	return &Squarespace{instanceID: instanceID, config: cfg, client: client}, nil
}

func (s *Squarespace) Name() string        { return "Squarespace Pay Links" }
func (s *Squarespace) ProviderKey() string { return payment.TypeSquarespace }
func (s *Squarespace) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeSquarespace}
}
func (s *Squarespace) MerchantIdentityMetadata() map[string]string {
	metadata := map[string]string{"website_id": s.config["websiteId"], "currency": "GBP", "order_scope_mode": s.config["orderScopeMode"]}
	if s.config["orderScopeMode"] == SquarespaceScopeFixedProduct {
		metadata["product_id"] = s.config["productId"]
	} else {
		metadata["payment_purpose"] = s.config["paymentPurpose"]
		metadata["expected_service_name"] = s.config["expectedServiceName"]
	}
	return metadata
}

func (s *Squarespace) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	amount, err := (SquarespaceMoney{Currency: "GBP", Value: req.Amount}).MinorUnits("GBP")
	if err != nil || amount <= 0 {
		return nil, squarespaceError("invalid_create_amount")
	}
	if req.PaymentType != "" && req.PaymentType != payment.TypeSquarespace {
		return nil, squarespaceError("unsupported_payment_type")
	}
	return &payment.CreatePaymentResponse{PayURL: s.config["payLinkUrl"], Currency: "GBP", ResultType: payment.CreatePaymentResultOrderCreated}, nil
}

func (s *Squarespace) QueryOrder(ctx context.Context, orderID string) (*payment.QueryOrderResponse, error) {
	order, err := s.client.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if err := ValidateSquarespaceOrderScope(order, s.config); err != nil {
		return nil, err
	}
	documents, err := s.client.ListAllTransactionsForOrder(ctx, orderID, squarespaceDefaultMaxPages)
	if err != nil {
		return nil, err
	}
	result, err := EvaluateSquarespaceOrder(order, documents)
	if err != nil {
		return nil, err
	}
	for key, value := range s.MerchantIdentityMetadata() {
		result.Metadata[key] = value
	}
	if s.config["orderScopeMode"] == SquarespaceScopeDedicatedSiteService {
		result.Metadata["actual_product_id"] = strings.ToLower(order.LineItems[0].ProductID)
	}
	return result, nil
}

func (*Squarespace) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, ErrSquarespaceUnsupported
}
func (*Squarespace) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, ErrSquarespaceUnsupported
}

func addSquarespaceMinor(sum *int64, value int64) error {
	if value < 0 || *sum > math.MaxInt64-value {
		return squarespaceError("money_overflow")
	}
	*sum += value
	return nil
}

// EvaluateSquarespaceOrder reuses the same complete financial verification in
// provider queries and batch synchronization. It does not establish ownership;
// bridge/core must separately bind the nonce and immutable quote to a user.
func EvaluateSquarespaceOrder(order *SquarespaceOrder, documents []SquarespaceTransactionDocument) (*payment.QueryOrderResponse, error) {
	if order == nil || !squarespaceIDPattern.MatchString(order.ID) {
		return nil, squarespaceError("invalid_upstream_order")
	}
	if order.TestMode == nil || *order.TestMode {
		return nil, squarespaceError("test_order_or_missing_test_flag")
	}
	gross, err := order.GrandTotal.MinorUnits("GBP")
	if err != nil || gross <= 0 {
		return nil, squarespaceError("invalid_order_total")
	}
	refunded, err := order.RefundedTotal.MinorUnits("GBP")
	if err != nil || refunded < 0 || refunded > gross {
		return nil, squarespaceError("invalid_order_refund")
	}
	status := payment.ProviderStatusPending
	switch order.PaymentState {
	case "PAID":
		status = payment.ProviderStatusPaid
	case "REFUNDED":
		if refunded == 0 {
			return nil, squarespaceError("inconsistent_refund_state")
		}
		status = payment.ProviderStatusRefunded
	case "NOT_CHARGED", "AUTHORIZED", "PENDING", "REFUND_PENDING", "REFUND_FAILED":
	case "FAILED", "CANCELED":
		status = payment.ProviderStatusFailed
	default:
		return nil, squarespaceError("unknown_payment_state")
	}
	result := &payment.QueryOrderResponse{TradeNo: order.ID, Status: status, Amount: float64(gross) / 100, Currency: "GBP", Metadata: map[string]string{
		"currency": "GBP", "gross_minor": strconv.FormatInt(gross, 10), "refunded_minor": strconv.FormatInt(refunded, 10), "checkout_reference": order.TopUpReference,
	}}
	if refunded > 0 {
		result.Status = payment.ProviderStatusRefunded
	}
	if len(documents) == 0 && order.PaymentState != "PAID" && refunded == 0 {
		return result, nil
	}
	// The official grouping is one Document per order. Never combine unrelated
	// or duplicate documents to manufacture a fully paid grand total.
	if len(documents) != 1 {
		return nil, squarespaceError("transaction_document_missing_or_not_unique")
	}
	document := documents[0]
	if !squarespaceIDPattern.MatchString(document.ID) || document.SalesOrderID != order.ID {
		return nil, squarespaceError("transaction_identity_mismatch")
	}
	if document.Voided == nil || *document.Voided || document.PaymentGatewayError != "" {
		return nil, squarespaceError("voided_or_gateway_error_transaction")
	}
	docTotal, err := document.Total.MinorUnits("GBP")
	if err != nil || docTotal != gross {
		return nil, squarespaceError("document_total_inconsistent")
	}
	docNet, err := document.TotalNetPayment.MinorUnits("GBP")
	if err != nil {
		return nil, squarespaceError("document_net_invalid")
	}
	var totalPaid, totalFeeGross, totalFeeNet, totalFeeRefunds, totalRefunds int64
	var paymentIDs, externalIDs, paidOnValues []string
	seenPayments, seenExternal := make(map[string]bool), make(map[string]bool)
	var latest time.Time
	var earliest time.Time
	latestRaw := ""
	earliestRaw := ""
	for _, payer := range document.Payments {
		amount, err := payer.Amount.MinorUnits("GBP")
		if err != nil || amount < 0 {
			return nil, squarespaceError("invalid_payment_amount")
		}
		if amount == 0 {
			continue
		}
		// This rollout is backed by the observed Squarespace Payments contract.
		// Other gateways' generic netAmount semantics are intentionally unsupported.
		if payer.Provider != SquarespacePaymentGateway {
			return nil, squarespaceError("unsupported_payment_gateway")
		}
		externalKey := payer.Provider + ":" + payer.ExternalTransactionID
		if !squarespaceIDPattern.MatchString(payer.ID) || seenPayments[payer.ID] || !squarespaceIDPattern.MatchString(payer.ExternalTransactionID) || seenExternal[externalKey] {
			return nil, squarespaceError("payment_identity_missing_or_duplicate")
		}
		seenPayments[payer.ID], seenExternal[externalKey] = true, true
		paidAt, err := time.Parse(time.RFC3339Nano, payer.PaidOn)
		if err != nil || paidAt.IsZero() {
			return nil, squarespaceError("invalid_official_paid_on")
		}
		if latest.IsZero() || paidAt.After(latest) {
			latest, latestRaw = paidAt, payer.PaidOn
		}
		if earliest.IsZero() || paidAt.Before(earliest) {
			earliest, earliestRaw = paidAt, payer.PaidOn
		}
		net, err := payer.NetAmount.MinorUnits("GBP")
		if err != nil {
			return nil, squarespaceError("invalid_payment_net")
		}
		paymentRefund, err := payer.RefundedAmount.MinorUnits("GBP")
		if err != nil || paymentRefund < 0 || paymentRefund > amount {
			return nil, squarespaceError("invalid_payment_refund")
		}
		refundRecords, err := squarespaceRefundRecords(payer.Refunds)
		if err != nil || refundRecords != paymentRefund {
			return nil, squarespaceError("refund_records_inconsistent")
		}
		var feeGross, feeNet, feeRefund int64
		for _, fee := range payer.ProcessingFees {
			original, err := fee.Amount.MinorUnits("GBP")
			if err != nil || original < 0 {
				return nil, squarespaceError("invalid_processing_fee")
			}
			remaining, err := fee.NetAmount.MinorUnits("GBP")
			if err != nil || remaining < 0 || remaining > original {
				return nil, squarespaceError("invalid_net_processing_fee")
			}
			returned, err := fee.RefundedAmount.MinorUnits("GBP")
			if err != nil || returned < 0 || returned > original || remaining != original-returned {
				return nil, squarespaceError("processing_fee_refund_inconsistent")
			}
			recordTotal, err := squarespaceRefundRecords(fee.FeeRefunds)
			if err != nil || recordTotal != returned {
				return nil, squarespaceError("fee_refund_records_inconsistent")
			}
			for _, sum := range []struct {
				target *int64
				value  int64
			}{{&feeGross, original}, {&feeNet, remaining}, {&feeRefund, returned}} {
				if err := addSquarespaceMinor(sum.target, sum.value); err != nil {
					return nil, err
				}
			}
		}
		if net != amount-paymentRefund-feeNet {
			return nil, squarespaceError("squarespace_payment_net_inconsistent")
		}
		for _, sum := range []struct {
			target *int64
			value  int64
		}{{&totalPaid, amount}, {&totalFeeGross, feeGross}, {&totalFeeNet, feeNet}, {&totalFeeRefunds, feeRefund}, {&totalRefunds, paymentRefund}} {
			if err := addSquarespaceMinor(sum.target, sum.value); err != nil {
				return nil, err
			}
		}
		paymentIDs = append(paymentIDs, payer.ID)
		externalIDs = append(externalIDs, payer.ExternalTransactionID)
		paidOnValues = append(paidOnValues, payer.PaidOn)
	}
	if totalPaid != gross || totalRefunds != refunded || latest.IsZero() {
		return nil, squarespaceError("order_payment_totals_inconsistent")
	}
	// Independent document-layer accounting. Do not assume generic gateway
	// payment.netAmount is post-fee, or sum it to define Document.totalNetPayment.
	expectedNet := gross - refunded - totalFeeNet
	if docNet != expectedNet {
		return nil, squarespaceError("document_net_inconsistent")
	}
	result.PaidAt = latestRaw
	result.Metadata["squarespace_paid_on"] = latestRaw
	result.Metadata["squarespace_first_paid_on"] = earliestRaw
	result.Metadata["net_minor"] = strconv.FormatInt(expectedNet, 10)
	result.Metadata["processing_fee_minor"] = strconv.FormatInt(totalFeeNet, 10)
	result.Metadata["processing_fee_gross_minor"] = strconv.FormatInt(totalFeeGross, 10)
	result.Metadata["processing_fee_refunded_minor"] = strconv.FormatInt(totalFeeRefunds, 10)
	result.Metadata["payment_net_rule"] = "SQSP_PAYMENTS_post_fee_after_refunds"
	for key, values := range map[string][]string{"upstream_document_ids": {document.ID}, "upstream_payment_ids": paymentIDs, "upstream_external_transaction_ids": externalIDs, "upstream_paid_on": paidOnValues} {
		encoded, _ := json.Marshal(values)
		result.Metadata[key] = string(encoded)
	}
	return result, nil
}

func squarespaceRefundRecords(records []SquarespaceRefund) (int64, error) {
	var total int64
	seen := make(map[string]bool)
	for _, record := range records {
		amount, err := record.Amount.MinorUnits("GBP")
		if err != nil || amount <= 0 {
			return 0, squarespaceError("invalid_refund_record_amount")
		}
		// IDs/timestamps are retained if supplied; amount/currency is mandatory.
		// The fee-refund schema does not guarantee the order-refund identifier shape.
		if record.ID != "" {
			if !squarespaceIDPattern.MatchString(record.ID) || seen[record.ID] {
				return 0, squarespaceError("duplicate_or_invalid_refund_record")
			}
			seen[record.ID] = true
		}
		if record.RefundedOn != "" {
			if at, err := time.Parse(time.RFC3339Nano, record.RefundedOn); err != nil || at.IsZero() {
				return 0, squarespaceError("invalid_refund_record_time")
			}
		}
		if err := addSquarespaceMinor(&total, amount); err != nil {
			return 0, err
		}
	}
	return total, nil
}
