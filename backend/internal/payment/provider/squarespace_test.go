package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

type squarespaceTestTransport func(*http.Request) (*http.Response, error)

func (f squarespaceTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func squarespaceTestResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func squarespaceTestClient(t *testing.T, callback squarespaceTestTransport) *SquarespaceClient {
	t.Helper()
	client, err := NewSquarespaceClient("site_1", SquarespaceReferenceFieldLabel, SquarespaceTokenSourceFunc(func(context.Context) (string, error) { return "MOCK_AT", nil }))
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = &http.Client{Transport: callback, Timeout: time.Second}
	return client
}

func TestSquarespaceMoneyExactMinorUnits(t *testing.T) {
	for _, test := range []struct {
		currency, value string
		want            int64
		bad             bool
	}{
		{"GBP", "1.00", 100, false}, {"GBP", "12.3", 1230, false}, {"GBP", "0", 0, false},
		{"USD", "1.00", 0, true}, {"GBP", "0.001", 0, true}, {"GBP", "-1.00", -100, false},
		{"GBP", "NaN", 0, true}, {"GBP", "1e2", 0, true}, {"GBP", "1.00 ", 0, true},
		{"GBP", "92233720368547758.08", 0, true},
	} {
		t.Run(test.currency+test.value, func(t *testing.T) {
			got, err := (SquarespaceMoney{test.currency, test.value}).MinorUnits("GBP")
			if (err != nil) != test.bad || (!test.bad && got != test.want) {
				t.Fatalf("got %d/%v", got, err)
			}
		})
	}
}

func squarespacePaidFixture() (*SquarespaceOrder, []SquarespaceTransactionDocument) {
	f := false
	money := func(value string) SquarespaceMoney { return SquarespaceMoney{"GBP", value} }
	order := &SquarespaceOrder{ID: "order_1", OrderNumber: "1", PaymentState: "PAID", TestMode: &f, GrandTotal: money("1.00"), RefundedTotal: money("0.00"), TopUpReference: "sq_0123456789abcdef", ReferenceStatus: "valid"}
	documents := []SquarespaceTransactionDocument{{ID: "document_1", SalesOrderID: "order_1", Voided: &f, Total: money("1.00"), TotalNetPayment: money("0.73"), Payments: []SquarespacePayment{
		{ID: "payment_1", ExternalTransactionID: "pi_payment_1", Provider: SquarespacePaymentGateway, PaidOn: "2026-09-29T23:40:00.123Z", Amount: money("1.00"), NetAmount: money("0.73"), RefundedAmount: money("0.00"), ProcessingFees: []SquarespaceProcessingFee{{Amount: money("0.27"), NetAmount: money("0.27"), RefundedAmount: money("0.00")}}},
	}}}
	return order, documents
}

func TestSquarespaceEvaluateOfficialPaymentEvidence(t *testing.T) {
	order, documents := squarespacePaidFixture()
	result, err := EvaluateSquarespaceOrder(order, documents)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != payment.ProviderStatusPaid || result.TradeNo != "order_1" || result.Amount != 1 || result.Currency != "GBP" || result.PaidAt != documents[0].Payments[0].PaidOn {
		t.Fatalf("incorrect payment evidence: %#v", result)
	}
	for key, want := range map[string]string{"currency": "GBP", "gross_minor": "100", "net_minor": "73", "processing_fee_minor": "27", "refunded_minor": "0", "checkout_reference": order.TopUpReference, "squarespace_paid_on": result.PaidAt} {
		if result.Metadata[key] != want {
			t.Errorf("%s=%q want %q", key, result.Metadata[key], want)
		}
	}
	if result.Metadata["upstream_external_transaction_ids"] != `["pi_payment_1"]` {
		t.Fatal("lost processor payment ID")
	}
}

func TestSquarespaceEvaluateAllPositivePayments(t *testing.T) {
	order, documents := squarespacePaidFixture()
	first := documents[0].Payments[0]
	first.Amount.Value = "0.40"
	first.NetAmount.Value = "0.20"
	first.ProcessingFees[0].Amount.Value = "0.20"
	first.ProcessingFees[0].NetAmount.Value = "0.20"
	second := first
	second.ID = "payment_2"
	second.ExternalTransactionID = "pi_payment_2"
	second.PaidOn = "2026-09-29T23:41:00.456Z"
	second.Amount.Value = "0.60"
	second.NetAmount.Value = "0.53"
	second.ProcessingFees = []SquarespaceProcessingFee{{Amount: SquarespaceMoney{"GBP", "0.07"}, NetAmount: SquarespaceMoney{"GBP", "0.07"}, RefundedAmount: SquarespaceMoney{"GBP", "0.00"}}}
	documents[0].Payments = []SquarespacePayment{first, second}
	result, err := EvaluateSquarespaceOrder(order, documents)
	if err != nil {
		t.Fatal(err)
	}
	if result.PaidAt != second.PaidOn || result.Metadata["processing_fee_minor"] != "27" || result.Metadata["upstream_payment_ids"] != `["payment_1","payment_2"]` {
		t.Fatalf("did not aggregate every payment: %#v", result)
	}
}

func TestSquarespaceEvaluationRejectsUnsafeEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*SquarespaceOrder, []SquarespaceTransactionDocument)
	}{
		{"testmode", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { v := true; o.TestMode = &v }},
		{"missing_testmode", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { o.TestMode = nil }},
		{"wrong_currency", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].Amount.Currency = "USD"
		}},
		{"fractional_minor", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { o.GrandTotal.Value = "1.001" }},
		{"mismatched_order", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { d[0].SalesOrderID = "foreign_order" }},
		{"voided", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { v := true; d[0].Voided = &v }},
		{"missing_voided", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { d[0].Voided = nil }},
		{"gateway_error", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { d[0].PaymentGatewayError = "error" }},
		{"invalid_paidon", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { d[0].Payments[0].PaidOn = "" }},
		{"fee_net_mismatch", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].NetAmount.Value = "0.74"
		}},
		{"doc_net_mismatch", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { d[0].TotalNetPayment.Value = "0.74" }},
		{"paid_under_total", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { o.GrandTotal.Value = "2.00" }},
		{"hidden_refund", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].RefundedAmount.Value = "0.50"
		}},
		{"unknown_gateway", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) { d[0].Payments[0].Provider = "UNKNOWN" }},
		{"unverified_square_alias", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].Provider = "SQUARESPACE"
		}},
		{"generic_stripe_gateway", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].Provider = "STRIPE"
			d[0].Payments[0].NetAmount.Value = "1.00"
		}},
		{"fee_net_missing", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].ProcessingFees[0].NetAmount = SquarespaceMoney{}
		}},
		{"fee_refund_missing", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].ProcessingFees[0].RefundedAmount = SquarespaceMoney{}
		}},
		{"fee_refund_inconsistent", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments[0].ProcessingFees[0].RefundedAmount.Value = "0.10"
		}},
		{"duplicate_payment", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			d[0].Payments = append(d[0].Payments, d[0].Payments[0])
		}},
		{"duplicate_processor_payment", func(o *SquarespaceOrder, d []SquarespaceTransactionDocument) {
			p := d[0].Payments[0]
			p.ID = "different_id"
			d[0].Payments = append(d[0].Payments, p)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			o, d := squarespacePaidFixture()
			test.change(o, d)
			if _, err := EvaluateSquarespaceOrder(o, d); err == nil {
				t.Fatal("unsafe payment accepted")
			}
		})
	}
}

func TestSquarespaceRefundIsNeverPaidAndNoReferenceCanBeFinanciallyChecked(t *testing.T) {
	order, documents := squarespacePaidFixture()
	order.PaymentState = "REFUNDED"
	order.RefundedTotal.Value = "0.50"
	documents[0].Payments[0].RefundedAmount.Value = "0.50"
	documents[0].Payments[0].NetAmount.Value = "0.23"
	documents[0].TotalNetPayment.Value = "0.23"
	documents[0].Payments[0].Refunds = []SquarespaceRefund{{ID: "refund_1", RefundedOn: "2026-09-30T00:00:00Z", Amount: SquarespaceMoney{"GBP", "0.50"}}}
	result, err := EvaluateSquarespaceOrder(order, documents)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != payment.ProviderStatusRefunded {
		t.Fatal("refunded payment treated as paid")
	}
	order, documents = squarespacePaidFixture()
	order.TopUpReference = ""
	order.ReferenceStatus = "missing"
	result, err = EvaluateSquarespaceOrder(order, documents)
	if err != nil || result.Status != payment.ProviderStatusPaid {
		t.Fatal("authenticated receipt claim cannot check financial evidence")
	}
}

func TestSquarespaceClientPrivacyReferenceAndClaimReads(t *testing.T) {
	var calls int
	client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != "GET" || request.URL.Host != "api.squarespace.com" || request.Header.Get("Authorization") != "Bearer MOCK_AT" || request.Header.Get("User-Agent") == "" {
			t.Fatal("unsafe request")
		}
		if request.URL.Path == "/1.0/authorization/website" {
			return squarespaceTestResponse(200, `{"id":"site_1"}`), nil
		}
		return squarespaceTestResponse(200, `{"id":"order_1","orderNumber":"1","customerEmail":"private@example.test","formSubmission":[{"label":"RynexAI top-up reference","value":"sq_0123456789abcdef"},{"label":"email","value":"private@example.test"}]}`), nil
	})
	normal, err := client.GetOrder(context.Background(), "order_1")
	if err != nil {
		t.Fatal(err)
	}
	if normal.TopUpReference != "sq_0123456789abcdef" || normal.CustomerEmail != "" {
		t.Fatal("ordinary order read retained customer email")
	}
	claim, err := client.GetOrderForReceiptClaim(context.Background(), "order_1")
	if err != nil {
		t.Fatal(err)
	}
	if claim.CustomerEmail != "private@example.test" || claim.OrderNumber != "1" {
		t.Fatal("receipt claim missing in-memory proof")
	}
	encoded, _ := json.Marshal(claim)
	if strings.Contains(string(encoded), "private@") || strings.Contains(fmt.Sprintf("%v %+v %#v", *claim, *claim, *claim), "private@") {
		t.Fatal("customer email leaked to JSON or diagnostics")
	}
	if calls != 3 {
		t.Fatalf("website identity was not bound/cached: %d calls", calls)
	}
}

func TestSquarespaceReferenceExtractionRejectsDuplicatesAndEmailValues(t *testing.T) {
	for _, form := range []string{
		`[{"label":"RynexAI top-up reference","value":"private@example.test"}]`,
		`[{"label":"RynexAI top-up reference","value":"sq_0123456789abcdef"},{"label":"RynexAI top-up reference","value":"sq_0123456789abcdef"}]`,
	} {
		var raw squarespaceRawOrder
		if err := json.Unmarshal([]byte(`{"id":"order_1","formSubmission":`+form+`}`), &raw); err != nil {
			t.Fatal(err)
		}
		public := raw.publicOrder(SquarespaceReferenceFieldLabel)
		if public.TopUpReference != "" || public.ReferenceStatus == "valid" {
			t.Fatal("unsafe reference accepted")
		}
	}
}

func TestSquarespaceClientRejectsWebsiteMismatchBeforeOrders(t *testing.T) {
	calls := 0
	client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Path != "/1.0/authorization/website" {
			t.Fatal("queried foreign website orders")
		}
		return squarespaceTestResponse(200, `{"id":"foreign"}`), nil
	})
	_, err := client.ListOrders(context.Background(), "", "", "")
	if err == nil || calls != 1 {
		t.Fatal("website mismatch was not closed")
	}
}

func TestSquarespaceClientReverifiesChangedToken(t *testing.T) {
	calls := 0
	client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/1.0/authorization/website" {
			calls++
			if request.Header.Get("Authorization") == "Bearer MOCK_AT_NEW" {
				return squarespaceTestResponse(200, `{"id":"foreign"}`), nil
			}
			return squarespaceTestResponse(200, `{"id":"site_1"}`), nil
		}
		return squarespaceTestResponse(200, `{"id":"order_1"}`), nil
	})
	if _, err := client.GetOrder(context.Background(), "order_1"); err != nil {
		t.Fatal(err)
	}
	client.tokens = SquarespaceTokenSourceFunc(func(context.Context) (string, error) { return "MOCK_AT_NEW", nil })
	if _, err := client.GetOrder(context.Background(), "order_1"); err == nil || calls != 2 {
		t.Fatal("new token did not trigger website verification")
	}
}

func TestSquarespaceClientRedirectAndRateLimitNeverExposeResponse(t *testing.T) {
	for _, code := range []int{302, 429, 401, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
				calls++
				response := squarespaceTestResponse(code, "MOCK_SECRET private@example.test")
				response.Header.Set("Location", "https://evil.example/")
				response.Header.Set("Retry-After", "17")
				return response, nil
			})
			err := client.VerifyWebsite(context.Background())
			var upstream *SquarespaceAPIError
			if !errors.As(err, &upstream) || upstream.HTTPStatus != code || calls != 1 || strings.Contains(err.Error(), "MOCK_SECRET") || strings.Contains(err.Error(), "private@") {
				t.Fatalf("unsafe error handling: %v", err)
			}
			if code == 429 && (upstream.Kind != "rate_limited" || upstream.RetryAfter != 17*time.Second) {
				t.Fatal("lost bounded retry delay")
			}
		})
	}
}

func TestSquarespaceOrderWindowAndBoundedCursorTransactions(t *testing.T) {
	requests := 0
	client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/1.0/authorization/website" {
			return squarespaceTestResponse(200, `{"id":"site_1"}`), nil
		}
		requests++
		if request.URL.Path == "/1.0/commerce/orders" {
			if request.URL.Query().Get("modifiedAfter") == "" || request.URL.Query().Get("modifiedBefore") == "" {
				t.Fatal("unbounded initial order window")
			}
			return squarespaceTestResponse(200, `{"result":[],"pagination":{"hasNextPage":false,"nextPageCursor":null}}`), nil
		}
		if request.URL.Query().Get("cursor") == "" {
			if request.URL.Query().Get("orderId") != "order_1" {
				t.Fatal("missing order filter")
			}
		} else if request.URL.Query().Get("orderId") != "" {
			t.Fatal("cursor mixed with original filter")
		}
		return squarespaceTestResponse(200, `{"documents":[],"pagination":{"hasNextPage":true,"nextPageCursor":"same_cursor","nextPageUrl":"https://evil.example/"}}`), nil
	})
	if _, err := client.ListOrders(context.Background(), "cursor", "2026-09-29T00:00:00Z", "2026-09-30T00:00:00Z"); err == nil {
		t.Fatal("mixed cursor/window accepted")
	}
	if _, err := client.ListOrders(context.Background(), "", "2026-09-29T00:00:00Z", ""); err == nil {
		t.Fatal("half window accepted")
	}
	if _, err := client.ListOrders(context.Background(), "", "2026-09-29T00:00:00Z", "2026-09-30T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListAllTransactionsForOrder(context.Background(), "order_1", 3); err == nil {
		t.Fatal("cursor loop accepted")
	}
	if requests != 3 {
		t.Fatalf("unexpected pagination requests: %d", requests)
	}
}

func TestSquarespaceReceiptLookupRequiresFullBoundedUniqueScan(t *testing.T) {
	calls := 0
	client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/1.0/authorization/website" {
			return squarespaceTestResponse(200, `{"id":"site_1"}`), nil
		}
		calls++
		if request.URL.Query().Get("cursor") == "" {
			return squarespaceTestResponse(200, `{"result":[{"id":"order_1","orderNumber":"1","customerEmail":"private@example.test"}],"pagination":{"hasNextPage":true,"nextPageCursor":"second"}}`), nil
		}
		return squarespaceTestResponse(200, `{"result":[],"pagination":{"hasNextPage":false}}`), nil
	})
	result, err := client.FindOrderByNumber(context.Background(), "00001", "2026-09-29T00:00:00Z", "2026-09-30T00:00:00Z", 2)
	if err != nil || result.ID != "order_1" || result.CustomerEmail != "" || calls != 2 {
		t.Fatalf("incorrect bounded receipt scan: %v", err)
	}
	if _, err := client.FindOrderByNumber(context.Background(), "00001", "2026-09-29T00:00:00Z", "2026-09-30T00:00:00Z", 1); err == nil {
		t.Fatal("incomplete scan accepted a receipt")
	}
	if _, err := CanonicalSquarespaceReceiptNumber("private@example.test"); err == nil {
		t.Fatal("receipt accepted email input")
	}
}

func TestSquarespaceCreateIsStaticNoTradeNoAndNoWriteCapabilities(t *testing.T) {
	provider, err := CreateProvider(payment.TypeSquarespace, "instance_1", map[string]string{"websiteId": "site_1", "productId": "0123456789abcdef01234567", "payLinkUrl": "https://ritelt.squarespace.com/pay-link/", "currency": "GBP"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{OrderID: "internal_order", Amount: "1.00", PaymentType: payment.TypeSquarespace})
	if err != nil || result.TradeNo != "" || result.Currency != "GBP" || result.PayURL != "https://ritelt.squarespace.com/pay-link/" {
		t.Fatalf("unexpected create: %v", err)
	}
	if _, err := provider.QueryOrder(context.Background(), "order_1"); err == nil {
		t.Fatal("unconfigured OAuth query succeeded")
	}
	if _, err := provider.VerifyNotification(context.Background(), "", nil); !errors.Is(err, ErrSquarespaceUnsupported) {
		t.Fatal("unsupported notification was accepted")
	}
	if _, err := provider.Refund(context.Background(), payment.RefundRequest{}); !errors.Is(err, ErrSquarespaceUnsupported) {
		t.Fatal("unsupported refund was accepted")
	}
	for _, payURL := range []string{"http://ritelt.squarespace.com/pay-link/", "https://ritelt.squarespace.com.evil.example/pay-link/", "https://u:p@ritelt.squarespace.com/pay-link/", "https://ritelt.squarespace.com:443/pay-link/", "https://ritelt.squarespace.com/pay-link/#secret"} {
		if _, err := NewSquarespace("instance", map[string]string{"websiteId": "site_1", "productId": "0123456789abcdef01234567", "payLinkUrl": payURL}); err == nil {
			t.Fatal("unsafe checkout URL accepted")
		}
	}
}

func TestSquarespaceMoneyAcceptsExactJSONStringsAndNumbers(t *testing.T) {
	for _, raw := range []string{`{"currency":"GBP","value":"0.27"}`, `{"currency":"GBP","value":0.27}`, `{"currency":"GBP","value":2.7e-1}`} {
		var money SquarespaceMoney
		if err := json.Unmarshal([]byte(raw), &money); err != nil {
			t.Fatal(err)
		}
		minor, err := money.MinorUnits("GBP")
		if err != nil || minor != 27 {
			t.Fatalf("lost decimal precision: %d/%v", minor, err)
		}
	}
	for _, raw := range []string{`{"currency":"GBP","value":0.001}`, `{"currency":"GBP","value":null}`, `{"currency":"GBP","value":1e99999}`} {
		var money SquarespaceMoney
		if err := json.Unmarshal([]byte(raw), &money); err == nil {
			t.Fatal("unsupported numeric money accepted")
		}
	}
}

func TestSquarespaceFinancialRefundAndFeeRefundAccounting(t *testing.T) {
	order, documents := squarespacePaidFixture()
	order.PaymentState = "REFUNDED"
	order.RefundedTotal.Value = "0.50"
	pay := &documents[0].Payments[0]
	pay.RefundedAmount.Value = "0.50"
	pay.NetAmount.Value = "0.30"
	pay.Refunds = []SquarespaceRefund{{ID: "refund_1", Amount: SquarespaceMoney{"GBP", "0.50"}}}
	fee := &pay.ProcessingFees[0]
	fee.NetAmount.Value = "0.20"
	fee.RefundedAmount.Value = "0.07"
	fee.FeeRefunds = []SquarespaceRefund{{ID: "fee_refund_1", Amount: SquarespaceMoney{"GBP", "0.07"}}}
	documents[0].TotalNetPayment.Value = "0.30"
	result, err := EvaluateSquarespaceOrder(order, documents)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != payment.ProviderStatusRefunded || result.Metadata["net_minor"] != "30" || result.Metadata["processing_fee_minor"] != "20" || result.Metadata["processing_fee_refunded_minor"] != "7" {
		t.Fatal("incorrect independent refund/fee-refund accounting")
	}
	fee.RefundedAmount.Value = "0.06"
	if _, err := EvaluateSquarespaceOrder(order, documents); err == nil {
		t.Fatal("inconsistent fee/refund accepted")
	}
	order, documents = squarespacePaidFixture()
	order.PaymentState = "REFUNDED"
	order.RefundedTotal.Value = "1.00"
	pay = &documents[0].Payments[0]
	pay.RefundedAmount.Value = "1.00"
	pay.NetAmount.Value = "-0.27"
	pay.Refunds = []SquarespaceRefund{{ID: "refund_all", Amount: SquarespaceMoney{"GBP", "1.00"}}}
	documents[0].TotalNetPayment.Value = "-0.27"
	result, err = EvaluateSquarespaceOrder(order, documents)
	if err != nil || result.Status != payment.ProviderStatusRefunded || result.Metadata["net_minor"] != "-27" {
		t.Fatalf("full refund with retained processing fee was not audited: %v", err)
	}
}

func TestSquarespaceDocumentMustBeOneToOneWithOrder(t *testing.T) {
	order, documents := squarespacePaidFixture()
	second := documents[0]
	second.ID = "another_document"
	if _, err := EvaluateSquarespaceOrder(order, append(documents, second)); err == nil {
		t.Fatal("multiple documents used to prove one order")
	}
}

func TestSquarespaceNegativeNetDoesNotInventUnpaidStatus(t *testing.T) {
	order, documents := squarespacePaidFixture()
	order.GrandTotal.Value = "0.10"
	documents[0].Total.Value = "0.10"
	documents[0].TotalNetPayment.Value = "-0.17"
	documents[0].Payments[0].Amount.Value = "0.10"
	documents[0].Payments[0].NetAmount.Value = "-0.17"
	result, err := EvaluateSquarespaceOrder(order, documents)
	if err != nil || result.Status != payment.ProviderStatusPaid || result.Metadata["net_minor"] != "-17" {
		t.Fatalf("processor fee exceeding capture misclassified a real payment: %v", err)
	}
}

func TestSquarespaceProductBusinessProofIsSeparateAndExact(t *testing.T) {
	order, documents := squarespacePaidFixture()
	order.LineItems = []SquarespaceLineItem{{ID: "line_1", ProductID: "0123456789abcdef01234567"}}
	if err := ValidateSquarespaceTopupProduct(order, "0123456789abcdef01234567"); err != nil {
		t.Fatal(err)
	}
	order.LineItems[0].ProductID = "fedcba9876543210fedcba98"
	if err := ValidateSquarespaceTopupProduct(order, "0123456789abcdef01234567"); err == nil {
		t.Fatal("same-price other goods admitted as topup")
	}
	if _, err := EvaluateSquarespaceOrder(order, documents); err != nil {
		t.Fatal("pure financial/refund evidence was mixed with current product configuration")
	}
	order.LineItems = []SquarespaceLineItem{{ID: "line_1", ProductID: "0123456789abcdef01234567"}, {ID: "line_2", ProductID: "fedcba9876543210fedcba98"}}
	if err := ValidateSquarespaceTopupProduct(order, "0123456789abcdef01234567"); err == nil {
		t.Fatal("mixed basket admitted")
	}
	order.LineItems = nil
	if err := ValidateSquarespaceTopupProduct(order, "0123456789abcdef01234567"); err == nil {
		t.Fatal("missing product evidence admitted")
	}
	if _, err := NewSquarespace("instance", map[string]string{"websiteId": "site_1", "payLinkUrl": "https://ritelt.squarespace.com/pay-link/"}); err == nil {
		t.Fatal("missing configured product admitted")
	}
}

func TestSquarespaceOrdinaryOrderLineItemsExcludeCustomerProductText(t *testing.T) {
	var raw squarespaceRawOrder
	if err := json.Unmarshal([]byte(`{"id":"order_1","lineItems":[{"id":"line_1","productId":"0123456789abcdef01234567","variantId":null,"productName":"Private customer name","customizations":{"email":"private@example.test"}}]}`), &raw); err != nil {
		t.Fatal(err)
	}
	order := raw.publicOrder(SquarespaceReferenceFieldLabel)
	if len(order.LineItems) != 1 || order.LineItems[0].ProductID != "0123456789abcdef01234567" {
		t.Fatal("missing product identifier")
	}
	encoded, _ := json.Marshal(order)
	if strings.Contains(string(encoded), "Private customer") || strings.Contains(string(encoded), "private@") {
		t.Fatal("lineItem customer content retained")
	}
}

// This fixture is the allowlisted projection of one fresh authenticated API
// read recorded in verified-api-contract-full.json. It contains no customer
// fields; processor transaction IDs were consistently pseudonymized by capture.
func TestSquarespaceVerifiedRealGBP1FinancialAndProductContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/squarespace_verified_gbp1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Order         SquarespaceOrder                 `json:"order"`
		Documents     []SquarespaceTransactionDocument `json:"documents"`
		Pseudonymized bool                             `json:"external_transaction_ids_pseudonymized"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if !fixture.Pseudonymized || fixture.Order.CustomerEmail != "" {
		t.Fatal("network fixture must exclude customer data and pseudonymize processor IDs")
	}
	if err := ValidateSquarespaceTopupProduct(&fixture.Order, "6abc2d02ac3ba7447cdc0752"); err != nil {
		t.Fatalf("verified topup SKU rejected: %v", err)
	}
	proof, err := EvaluateSquarespaceOrder(&fixture.Order, fixture.Documents)
	if err != nil {
		t.Fatalf("actual API financial contract rejected: %v", err)
	}
	if proof.Status != payment.ProviderStatusPaid || proof.Amount != 1 || proof.Currency != "GBP" || proof.PaidAt != "2026-09-29T21:29:26.493Z" || proof.Metadata["gross_minor"] != "100" || proof.Metadata["net_minor"] != "73" || proof.Metadata["processing_fee_minor"] != "27" || proof.Metadata["refunded_minor"] != "0" {
		t.Fatal("actual GBP1 evidence not preserved")
	}
	if fixture.Documents[0].Payments[0].Provider != SquarespacePaymentGateway {
		t.Fatal("actual gateway differs from the sole whitelist")
	}
	if err := ValidateSquarespaceTopupProduct(&fixture.Order, "0123456789abcdef01234567"); err == nil {
		t.Fatal("same paid receipt accepted for another configured goods product")
	}
}

func TestSquarespaceConfiguredProductIDCanonical24Hex(t *testing.T) {
	provider, err := NewSquarespace("instance", map[string]string{"websiteId": "site_1", "productId": " ABCDEF0123456789ABCDEF01 ", "payLinkUrl": "https://ritelt.squarespace.com/pay-link/"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.MerchantIdentityMetadata()["product_id"] != "abcdef0123456789abcdef01" {
		t.Fatal("product identity not canonical")
	}
	for _, id := range []string{"short", "0123456789abcdef0123456z", "0123456789abcdef012345678", ""} {
		if _, err := NewSquarespace("instance", map[string]string{"websiteId": "site_1", "productId": id, "payLinkUrl": "https://ritelt.squarespace.com/pay-link/"}); err == nil {
			t.Fatal("malformed configured product ID admitted")
		}
	}
}

func TestSquarespaceConstrainedNonpaginatedFilteredTransactions(t *testing.T) {
	for _, test := range []struct {
		name, body, cursor string
		bad                bool
	}{
		{"live_single", `{"documents":[{"id":"doc_1","salesOrderId":"order_1"}]}`, "", false},
		{"empty", `{"documents":[]}`, "", false},
		{"foreign", `{"documents":[{"id":"doc_1","salesOrderId":"foreign"}]}`, "", true},
		{"multiple", `{"documents":[{"id":"doc_1","salesOrderId":"order_1"},{"id":"doc_2","salesOrderId":"order_1"}]}`, "", true},
		{"continuation", `{"documents":[{"id":"doc_1","salesOrderId":"order_1"}]}`, "second", true},
		{"hidden_cursor", `{"documents":[{"id":"doc_1","salesOrderId":"order_1"}],"pagination":{"nextPageCursor":"hidden"}}`, "", true},
		{"hidden_url", `{"documents":[{"id":"doc_1","salesOrderId":"order_1"}],"pagination":{"nextPageUrl":"https://evil.example/"}}`, "", true},
		{"false_with_cursor", `{"documents":[{"id":"doc_1","salesOrderId":"order_1"}],"pagination":{"hasNextPage":false,"nextPageCursor":"hidden"}}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := squarespaceTestClient(t, func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/1.0/authorization/website" {
					return squarespaceTestResponse(200, `{"id":"site_1"}`), nil
				}
				return squarespaceTestResponse(200, test.body), nil
			})
			page, err := client.ListTransactions(context.Background(), "order_1", test.cursor)
			if (err != nil) != test.bad {
				t.Fatalf("nonpagination compatibility=%v", err)
			}
			if err == nil && page.Pagination.HasNextPage {
				t.Fatal("invented a next page")
			}
		})
	}
}
