package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

func normalizeSquarespaceReceiptNumber(raw string) (string, error) {
	return provider.CanonicalSquarespaceReceiptNumber(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
}

func (s *PaymentConfigService) NewSquarespaceManagedClient(ctx context.Context, websiteID, referenceLabel string) (*provider.SquarespaceClient, error) {
	tokens, err := s.squarespaceTokenSource(ctx, websiteID)
	if err != nil {
		return nil, err
	}
	return provider.NewSquarespaceClient(websiteID, referenceLabel, tokens)
}

// FindSquarespaceReceiptOrder resolves an exact official sequence number, never
// a match by payment amount or payer email. The email is returned in memory only
// by the dedicated claim GET and is neither logged nor persisted here.
func (s *PaymentService) FindSquarespaceReceiptOrder(ctx context.Context, localOrder *dbent.PaymentOrder, receipt string) (*provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, error) {
	number, err := normalizeSquarespaceReceiptNumber(receipt)
	if err != nil {
		return nil, nil, err
	}
	quote := PaymentOrderRetailQuote(localOrder)
	snapshot := psOrderProviderSnapshot(localOrder)
	if quote == nil || snapshot == nil || snapshot.ProviderKey != "squarespace" || snapshot.MerchantID == "" || quote.IssuedAt.IsZero() {
		return nil, nil, errors.New("Squarespace quote unavailable")
	}
	instance, err := s.getOrderProviderInstance(ctx, localOrder)
	if err != nil || instance == nil {
		return nil, nil, errors.New("Squarespace provider instance unavailable")
	}
	cfg, err := s.configService.decryptConfig(instance.Config)
	if err != nil {
		return nil, nil, err
	}
	if cfg["websiteId"] != snapshot.MerchantID {
		return nil, nil, errors.New("Squarespace website binding changed")
	}
	client, err := s.configService.NewSquarespaceManagedClient(ctx, snapshot.MerchantID, cfg["referenceFieldLabel"])
	if err != nil {
		return nil, nil, err
	}
	if err = client.VerifyWebsite(ctx); err != nil {
		return nil, nil, err
	}
	rows, err := s.entClient.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(snapshot.MerchantID), paymentexternalorder.OrderNumberEQ(number)).Limit(2).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > 1 {
		return nil, nil, errors.New("ambiguous Squarespace receipt")
	}
	externalID := ""
	if len(rows) == 1 {
		externalID = rows[0].ExternalOrderID
	} else {
		cursor := ""
		matches := map[string]bool{}
		complete := false
		seen := map[string]bool{}
		for pageIndex := 0; pageIndex < 4; pageIndex++ {
			after, before := "", ""
			if cursor == "" {
				after = quote.IssuedAt.Add(-time.Second).UTC().Format(time.RFC3339Nano)
				before = time.Now().UTC().Format(time.RFC3339Nano)
			}
			page, fetchErr := client.ListOrders(ctx, cursor, after, before)
			if fetchErr != nil {
				return nil, nil, fetchErr
			}
			if page == nil {
				return nil, nil, errors.New("receipt lookup response missing")
			}
			for i := range page.Orders {
				remote := &page.Orders[i]
				candidate, parseErr := normalizeSquarespaceReceiptNumber(remote.OrderNumber)
				if parseErr == nil && candidate == number {
					matches[remote.ID] = true
					externalID = remote.ID
				}
			}
			if !page.Pagination.HasNextPage {
				complete = true
				break
			}
			cursor = page.Pagination.NextPageCursor
			if cursor == "" || seen[cursor] {
				return nil, nil, errors.New("receipt lookup pagination invalid")
			}
			seen[cursor] = true
		}
		if !complete {
			return nil, nil, errors.New("receipt lookup window exceeds bounded scan")
		}
		if len(matches) != 1 {
			return nil, nil, errors.New("receipt is missing or ambiguous")
		}
	}
	remote, err := client.GetOrderForReceiptClaim(ctx, externalID)
	if err != nil {
		return nil, nil, err
	}
	canonical, err := normalizeSquarespaceReceiptNumber(remote.OrderNumber)
	if err != nil || canonical != number {
		return nil, nil, errors.New("official receipt identity changed")
	}
	docs, err := client.ListAllTransactionsForOrder(ctx, remote.ID, 4)
	if err != nil {
		return nil, nil, err
	}
	return remote, docs, nil
}

type squarespaceVerifiedReceiptClaimKey struct{}
type squarespaceVerifiedReceiptClaimAuthority struct {
	userID, localOrderID       int64
	externalOrderID, quoteHash string
}

func withVerifiedSquarespaceReceiptClaim(ctx context.Context, userID, localOrderID int64, externalOrderID, quoteHash string) context.Context {
	return context.WithValue(ctx, squarespaceVerifiedReceiptClaimKey{}, squarespaceVerifiedReceiptClaimAuthority{userID: userID, localOrderID: localOrderID, externalOrderID: externalOrderID, quoteHash: quoteHash})
}
func SquarespaceReceiptQuoteHash(local *dbent.PaymentOrder) (string, error) {
	quote := PaymentOrderRetailQuote(local)
	if quote == nil {
		return "", errors.New("retail quote missing")
	}
	encoded, err := json.Marshal(quote)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// BindVerifiedReceiptClaim requires one-time OTP authority established by the
// claim service, then rechecks the frozen local quote before the unique binding.
func (s *PaymentService) BindVerifiedReceiptClaim(ctx context.Context, localOrder *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) error {
	if localOrder == nil || remote == nil {
		return errors.New("receipt claim proof missing")
	}
	authority, ok := ctx.Value(squarespaceVerifiedReceiptClaimKey{}).(squarespaceVerifiedReceiptClaimAuthority)
	if !ok || authority.userID != localOrder.UserID || authority.localOrderID != localOrder.ID || authority.externalOrderID != remote.ID {
		return errors.New("receipt claim authority missing or mismatched")
	}
	current, err := s.entClient.PaymentOrder.Get(ctx, localOrder.ID)
	if err != nil {
		return err
	}
	hash, err := SquarespaceReceiptQuoteHash(current)
	if err != nil {
		return err
	}
	if current.UserID != authority.userID || hash != authority.quoteHash {
		return errors.New("receipt claim authority does not match current quote")
	}
	proof, err := verifiedSquarespaceReceiptClaimProof(current, remote, docs)
	if err != nil {
		return err
	}
	snapshot := psOrderProviderSnapshot(current)
	instance, err := s.getOrderProviderInstance(ctx, current)
	if err != nil || instance == nil {
		return errors.New("receipt claim provider instance missing")
	}
	if !instance.Enabled || !s.configService.IsPaymentEnabled(ctx) {
		return errors.New("automatic credit disabled")
	}
	bridge := NewSquarespacePaymentBridge(s, nil, nil)
	ledger, err := bridge.observeOrder(ctx, snapshot.MerchantID, remote)
	if err != nil {
		return err
	}
	if ledger.LocalOrderID != nil && *ledger.LocalOrderID != current.ID {
		return errors.New("receipt already belongs to another local order")
	}
	ledger, err = bridge.bindExternalOrder(ctx, ledger, current, remote, proof, "receipt_otp")
	if err != nil {
		return err
	}
	return bridge.processExternalOrder(ctx, instance, snapshot.MerchantID, remote, docs, ledger)
}

// ReconcileBoundSquarespaceReceipt never grants a new claim. It only continues
// a fixed OTP-approved ledger binding after fresh official financial validation.
func (s *PaymentService) ReconcileBoundSquarespaceReceipt(ctx context.Context, local *dbent.PaymentOrder) (bool, error) {
	if local == nil || local.PaymentTradeNo == "" {
		return false, nil
	}
	snapshot := psOrderProviderSnapshot(local)
	if snapshot == nil || snapshot.MerchantID == "" {
		return false, errors.New("receipt website binding missing")
	}
	ledger, err := s.entClient.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.WebsiteIDEQ(snapshot.MerchantID), paymentexternalorder.LocalOrderIDEQ(local.ID), paymentexternalorder.ExternalOrderIDEQ(local.PaymentTradeNo), paymentexternalorder.BindingMethodEQ("receipt_otp")).Only(ctx)
	if err != nil {
		return false, errors.New("receipt has no verified ledger binding")
	}
	hash, err := SquarespaceReceiptQuoteHash(local)
	if err != nil || hash != ledger.QuoteHash {
		return false, errors.New("receipt quote binding changed")
	}
	instance, err := s.getOrderProviderInstance(ctx, local)
	if err != nil || instance == nil {
		return false, errors.New("receipt provider instance missing")
	}
	cfg, err := s.configService.decryptConfig(instance.Config)
	if err != nil {
		return false, err
	}
	if cfg["websiteId"] != snapshot.MerchantID {
		return false, errors.New("receipt website configuration changed")
	}
	client, err := s.configService.NewSquarespaceManagedClient(ctx, snapshot.MerchantID, cfg["referenceFieldLabel"])
	if err != nil {
		return false, err
	}
	remote, err := client.GetOrder(ctx, ledger.ExternalOrderID)
	if err != nil {
		return false, err
	}
	docs, err := client.ListAllTransactionsForOrder(ctx, remote.ID, 4)
	if err != nil {
		return false, err
	}
	proof, code := verifySquarespaceProof(remote, docs)
	if code != "" {
		return false, errors.New(code)
	}
	bridge := NewSquarespacePaymentBridge(s, nil, nil)
	if proof.RefundedMinor > 0 || remote.PaymentState != "PAID" {
		_ = bridge.anomaly(ctx, ledger.ID, "REFUND_OBSERVED_ON_RECHECK")
		return false, errors.New("receipt payment requires refund reconciliation")
	}
	if err = bridge.processExternalOrder(ctx, instance, snapshot.MerchantID, remote, docs, ledger); err != nil {
		return false, err
	}
	return true, nil
}
