package service

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

const squarespaceUSDScale int64 = 100000000

var squarespaceCheckoutReferencePattern = regexp.MustCompile(`^sub2_[0-9a-f]{32}$`)

type verifiedSquarespacePaymentProof struct {
	ID            string
	AmountMinor   int64
	RefundedMinor int64
	PaidAt        time.Time
}

type verifiedSquarespaceProof struct {
	Currency      string
	TotalMinor    int64
	RefundedMinor int64
	PaymentID     string // first positive payment; Payments is the authoritative set
	Payments      []verifiedSquarespacePaymentProof
	PaidAt        time.Time
}

// Financial semantics have one implementation in the provider. The bridge only
// projects verified identifiers/pennies/timestamps into its durable proof ledger.
func verifySquarespaceProof(order *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) (*verifiedSquarespaceProof, string) {
	if _, err := provider.EvaluateSquarespaceOrder(order, docs); err != nil {
		return nil, "PAYMENT_FINANCIAL_UNTRUSTED"
	}
	switch order.PaymentState {
	case "PAID", "REFUNDED", "REFUND_PENDING", "REFUND_FAILED":
	default:
		return nil, "NOT_PAID"
	}
	total, err := order.GrandTotal.MinorUnits("GBP")
	if err != nil {
		return nil, "PAYMENT_FINANCIAL_UNTRUSTED"
	}
	refunded, err := order.RefundedTotal.MinorUnits("GBP")
	if err != nil {
		return nil, "PAYMENT_FINANCIAL_UNTRUSTED"
	}
	proof := &verifiedSquarespaceProof{Currency: "GBP", TotalMinor: total, RefundedMinor: refunded}
	for _, doc := range docs {
		for _, paid := range doc.Payments {
			amount, parseErr := paid.Amount.MinorUnits("GBP")
			if parseErr != nil {
				return nil, "PAYMENT_FINANCIAL_UNTRUSTED"
			}
			if amount == 0 {
				continue
			}
			paymentRefunded, parseErr := paid.RefundedAmount.MinorUnits("GBP")
			if parseErr != nil {
				return nil, "PAYMENT_FINANCIAL_UNTRUSTED"
			}
			paidAt, parseErr := time.Parse(time.RFC3339Nano, paid.PaidOn)
			if parseErr != nil {
				return nil, "PAYMENT_FINANCIAL_UNTRUSTED"
			}
			proof.Payments = append(proof.Payments, verifiedSquarespacePaymentProof{ID: paid.ID, AmountMinor: amount, RefundedMinor: paymentRefunded, PaidAt: paidAt.UTC()})
			if proof.PaymentID == "" {
				proof.PaymentID = paid.ID
			}
			if proof.PaidAt.IsZero() || paidAt.After(proof.PaidAt) {
				proof.PaidAt = paidAt.UTC()
			}
		}
	}
	if len(proof.Payments) == 0 {
		return nil, "PAYMENT_PROOF_MISSING"
	}
	return proof, ""
}

func squarespaceFrozenProductID(local *dbent.PaymentOrder) (string, error) {
	quote := PaymentOrderRetailQuote(local)
	if local == nil || quote == nil {
		return "", errors.New("top-up product snapshot missing")
	}
	expected := PaymentOrderSquarespaceProductID(local)
	if expected == "" || quote.ProductID != expected {
		return "", errors.New("top-up product snapshot missing or inconsistent")
	}
	return expected, nil
}
func validateSquarespaceFrozenTopupProduct(local *dbent.PaymentOrder, remote *provider.SquarespaceOrder) string {
	scope, err := frozenSquarespaceOrderScope(local)
	if err != nil {
		return "TOPUP_PRODUCT_SNAPSHOT_INVALID"
	}
	if err = provider.ValidateSquarespaceOrderScope(remote, scope); err != nil {
		return "TOPUP_PRODUCT_MISMATCH"
	}
	return ""
}

func validateSquarespaceQuoteFinancialProof(local *dbent.PaymentOrder, proof *verifiedSquarespaceProof, websiteID string) string {
	if local == nil || proof == nil {
		return "LOCAL_ORDER_MISSING"
	}
	quote := PaymentOrderRetailQuote(local)
	if _, err := frozenSquarespaceOrderScope(local); err != nil {
		return "TOPUP_PRODUCT_SNAPSHOT_INVALID"
	}
	if quote == nil || quote.Currency != "GBP" || quote.IssuedAt.IsZero() || quote.ExpiresAt.IsZero() || !squarespaceCheckoutReferencePattern.MatchString(quote.CheckoutReference) || quote.CheckoutReference != local.OutTradeNo {
		return "QUOTE_MISSING_OR_INVALID"
	}
	snapshot := psOrderProviderSnapshot(local)
	if snapshot == nil || snapshot.ProviderKey != "squarespace" || snapshot.MerchantID != websiteID || local.OrderType != "balance" {
		return "WEBSITE_OR_ORDER_KIND_MISMATCH"
	}
	amount, err := squarespaceFloatUnits(quote.TotalAmountGBP, 100)
	if err != nil || amount != proof.TotalMinor {
		return "QUOTE_AMOUNT_MISMATCH"
	}
	payAmount, err := squarespaceFloatUnits(quote.PayAmount, 100)
	if err != nil || payAmount != amount {
		return "QUOTE_AMOUNT_MISMATCH"
	}
	credited, err := squarespaceFloatUnits(quote.CreditedAmountUSD, squarespaceUSDScale)
	localCredited, localErr := squarespaceFloatUnits(local.Amount, squarespaceUSDScale)
	if err != nil || localErr != nil || credited <= 0 || credited != localCredited {
		return "QUOTE_CREDIT_MISMATCH"
	}
	if proof.PaidAt.Before(quote.IssuedAt) || proof.PaidAt.After(quote.ExpiresAt) {
		return "PAYMENT_OUTSIDE_QUOTE"
	}
	for _, paid := range proof.Payments {
		if paid.PaidAt.Before(quote.IssuedAt) || paid.PaidAt.After(quote.ExpiresAt) {
			return "PAYMENT_OUTSIDE_QUOTE"
		}
	}
	return ""
}

func validateSquarespaceQuoteProof(local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, proof *verifiedSquarespaceProof, websiteID string) string {
	if remote == nil || local == nil {
		return "LOCAL_ORDER_MISSING"
	}
	if code := validateSquarespaceFrozenTopupProduct(local, remote); code != "" {
		return code
	}
	if remote.ReferenceStatus != "valid" || remote.TopUpReference != local.OutTradeNo {
		return "CHECKOUT_REFERENCE_MISMATCH"
	}
	return validateSquarespaceQuoteFinancialProof(local, proof, websiteID)
}

// ValidateSquarespaceReceiptClaimProof does not bind an account or consume OTP.
// It is the common financial precheck for challenge issuance and claim recheck.
func ValidateSquarespaceReceiptClaimProof(local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) error {
	_, err := verifiedSquarespaceReceiptClaimProof(local, remote, docs)
	return err
}
func verifiedSquarespaceReceiptClaimProof(local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) (*verifiedSquarespaceProof, error) {
	scope, err := frozenSquarespaceOrderScope(local)
	if err != nil {
		return nil, err
	}
	return verifiedSquarespaceReceiptClaimProofWithScope(local, remote, docs, scope)
}
func verifiedSquarespaceReceiptClaimProofWithScope(local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument, scope map[string]string) (*verifiedSquarespaceProof, error) {
	if local == nil || remote == nil {
		return nil, errors.New("receipt claim proof missing")
	}
	snapshot := psOrderProviderSnapshot(local)
	if snapshot == nil || snapshot.MerchantID == "" {
		return nil, errors.New("receipt claim website snapshot missing")
	}
	if PaymentOrderClaimMode(local) != "receipt_otp" {
		return nil, errors.New("receipt OTP claim mode required")
	}
	if _, err := normalizeSquarespaceReceiptNumber(remote.OrderNumber); err != nil {
		return nil, err
	}
	if err := provider.ValidateSquarespaceOrderScope(remote, scope); err != nil {
		return nil, err
	}
	proof, code := verifySquarespaceProof(remote, docs)
	if code != "" {
		return nil, errors.New(code)
	}
	if remote.PaymentState != "PAID" || proof.RefundedMinor != 0 {
		return nil, errors.New("receipt payment has refund activity or is not paid")
	}
	if proof.PaidAt.After(time.Now().Add(time.Minute)) {
		return nil, errors.New("receipt payment time is in the future")
	}
	if code = validateSquarespaceQuoteFinancialProof(local, proof, snapshot.MerchantID); code != "" {
		return nil, errors.New(code)
	}
	return proof, nil
}

func squarespaceFloatUnits(value float64, scale int64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, errors.New("invalid monetary value")
	}
	rational, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	if !ok {
		return 0, errors.New("invalid decimal monetary value")
	}
	rational.Mul(rational, new(big.Rat).SetInt64(scale))
	if !rational.IsInt() || !rational.Num().IsInt64() {
		return 0, errors.New("monetary value is not exact at configured precision")
	}
	return rational.Num().Int64(), nil
}

// Refund targets are derived from the frozen USD credit and cumulative GBP
// pennies. Round once at 8 decimals; a full refund always targets the exact credit.
func squarespaceRefundTarget(credited, total, refunded int64) (int64, error) {
	if credited < 0 || total <= 0 || refunded < 0 || refunded > total {
		return 0, errors.New("invalid proportional refund")
	}
	if refunded == total {
		return credited, nil
	}
	numerator := new(big.Int).Mul(big.NewInt(credited), big.NewInt(refunded))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, big.NewInt(total), remainder)
	if remainder.Mul(remainder, big.NewInt(2)).Cmp(big.NewInt(total)) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, errors.New("refund precision overflow")
	}
	return quotient.Int64(), nil
}
