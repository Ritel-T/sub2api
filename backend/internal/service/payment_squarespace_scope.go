package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
)

const squarespaceLegacyScopeBinding = "receipt_otp_legacy_scope"
const squarespaceLegacyScopeAudit = "ASSISTED_LEGACY_SCOPE_REVIEW"

type squarespaceScopeGrant struct {
	Config          map[string]string
	AssistedLegacy  bool
	ActualProductID string
}
type squarespaceLegacyScopeEvidence struct {
	LocalOrderID        int64  `json:"local_order_id"`
	UserID              int64  `json:"user_id"`
	ExternalOrderID     string `json:"external_order_id"`
	WebsiteID           string `json:"website_id"`
	CheckoutReference   string `json:"checkout_reference"`
	QuoteHash           string `json:"quote_hash"`
	ActualProductID     string `json:"actual_product_id"`
	OrderScopeMode      string `json:"order_scope_mode"`
	ExpectedServiceName string `json:"expected_service_name"`
	Purpose             string `json:"purpose"`
	OTPVerified         bool   `json:"otp_verified"`
	OTPPurpose          string `json:"otp_purpose"`
	PayerEmailHash      string `json:"payer_email_hash,omitempty"`
	AuthEmailHash       string `json:"auth_email_hash,omitempty"`
}

func frozenSquarespaceOrderScope(local *dbent.PaymentOrder) (map[string]string, error) {
	quote := PaymentOrderRetailQuote(local)
	snapshot := psOrderProviderSnapshot(local)
	if quote == nil || snapshot == nil || snapshot.ProviderKey != "squarespace" || snapshot.MerchantID == "" {
		return nil, errors.New("Squarespace frozen scope missing")
	}
	mode := quote.OrderScopeMode
	if mode == "" {
		mode = provider.SquarespaceScopeFixedProduct
	}
	cfg := map[string]string{"orderScopeMode": mode, "websiteId": snapshot.MerchantID, "currency": "GBP"}
	switch mode {
	case provider.SquarespaceScopeFixedProduct:
		if raw := psSnapshotStringValue(local.ProviderSnapshot["order_scope_mode"]); raw != "" && raw != mode {
			return nil, errors.New("Squarespace frozen scope changed")
		}
		pid, err := squarespaceFrozenProductID(local)
		if err != nil {
			return nil, err
		}
		cfg["productId"] = pid
	case provider.SquarespaceScopeDedicatedSiteService:
		if quote.Purpose != provider.SquarespaceBalanceTopupPurpose || quote.ExpectedServiceName != provider.SquarespaceExpectedServiceName || psSnapshotStringValue(local.ProviderSnapshot["order_scope_mode"]) != mode || psSnapshotStringValue(local.ProviderSnapshot["expected_service_name"]) != quote.ExpectedServiceName || psSnapshotStringValue(local.ProviderSnapshot["purpose"]) != quote.Purpose {
			return nil, errors.New("Squarespace dedicated scope snapshot changed")
		}
		cfg["paymentPurpose"] = quote.Purpose
		cfg["expectedServiceName"] = quote.ExpectedServiceName
	default:
		return nil, errors.New("Squarespace order scope is unsupported")
	}
	return cfg, nil
}
func (s *PaymentService) squarespaceReceiptScope(ctx context.Context, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, ledger *dbent.PaymentExternalOrder) (*squarespaceScopeGrant, error) {
	frozen, err := frozenSquarespaceOrderScope(local)
	if err != nil {
		return nil, err
	}
	if err = provider.ValidateSquarespaceOrderScope(remote, frozen); err == nil {
		return &squarespaceScopeGrant{Config: frozen}, nil
	}
	// An old fixed-SKU invoice is never generally upgraded. A current explicit
	// merchant review or a transactionally stored exact review is required.
	if frozen["orderScopeMode"] != provider.SquarespaceScopeFixedProduct {
		return nil, errors.New("Squarespace dedicated service proof mismatch")
	}
	if ledger != nil && ledger.BindingMethod == squarespaceLegacyScopeBinding {
		evidence, err := s.loadSquarespaceLegacyScopeEvidence(ctx, local, ledger)
		if err != nil {
			return nil, err
		}
		cfg := map[string]string{"orderScopeMode": evidence.OrderScopeMode, "paymentPurpose": evidence.Purpose, "expectedServiceName": evidence.ExpectedServiceName, "websiteId": evidence.WebsiteID, "currency": "GBP"}
		if remote == nil || remote.ID != evidence.ExternalOrderID || len(remote.LineItems) != 1 || strings.ToLower(remote.LineItems[0].ProductID) != evidence.ActualProductID || provider.ValidateSquarespaceOrderScope(remote, cfg) != nil {
			return nil, errors.New("reviewed legacy service identity changed")
		}
		return &squarespaceScopeGrant{Config: cfg, AssistedLegacy: true, ActualProductID: evidence.ActualProductID}, nil
	}
	if local == nil || remote == nil || s.configService == nil {
		return nil, errors.New("legacy Squarespace invoice has no review")
	}
	global, err := s.configService.GetPaymentConfig(ctx)
	if err != nil || !merchantTestUser(global, local.UserID) {
		return nil, errors.New("legacy review requires a current merchant test user")
	}
	inst, err := s.getOrderProviderInstance(ctx, local)
	if err != nil || inst == nil || !inst.Enabled {
		return nil, errors.New("legacy review provider unavailable")
	}
	cfg, err := s.configService.decryptConfig(inst.Config)
	if err != nil {
		return nil, err
	}
	if cfg["websiteId"] != frozen["websiteId"] || cfg["orderScopeMode"] != provider.SquarespaceScopeDedicatedSiteService || cfg["paymentPurpose"] != provider.SquarespaceBalanceTopupPurpose || cfg["expectedServiceName"] != provider.SquarespaceExpectedServiceName || cfg["reviewedLocalOrderId"] != strconv.FormatInt(local.ID, 10) || cfg["reviewedUserId"] != strconv.FormatInt(local.UserID, 10) || cfg["reviewedExternalOrderId"] != remote.ID || cfg["reviewedCheckoutReference"] != local.OutTradeNo || PaymentOrderRetailQuote(local).CheckoutReference != local.OutTradeNo {
		return nil, errors.New("legacy scope review tuple does not match this invoice")
	}
	if _, err = normalizeSquarespaceReceiptNumber(remote.OrderNumber); err != nil {
		return nil, err
	}
	if err = provider.ValidateSquarespaceOrderScope(remote, cfg); err != nil {
		return nil, err
	}
	return &squarespaceScopeGrant{Config: map[string]string{"orderScopeMode": provider.SquarespaceScopeDedicatedSiteService, "paymentPurpose": provider.SquarespaceBalanceTopupPurpose, "expectedServiceName": provider.SquarespaceExpectedServiceName, "websiteId": frozen["websiteId"], "currency": "GBP"}, AssistedLegacy: true, ActualProductID: strings.ToLower(remote.LineItems[0].ProductID)}, nil
}
func (s *PaymentService) loadSquarespaceLegacyScopeEvidence(ctx context.Context, local *dbent.PaymentOrder, ledger *dbent.PaymentExternalOrder) (*squarespaceLegacyScopeEvidence, error) {
	hash, err := SquarespaceReceiptQuoteHash(local)
	if err != nil || ledger == nil || ledger.LocalOrderID == nil || *ledger.LocalOrderID != local.ID || ledger.QuoteHash != hash || ledger.CheckoutReference != local.OutTradeNo {
		return nil, errors.New("legacy scope ledger changed")
	}
	rows, err := s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(local.ID, 10)), paymentauditlog.ActionEQ(squarespaceLegacyScopeAudit)).Limit(2).All(ctx)
	if err != nil || len(rows) != 1 {
		return nil, errors.New("legacy scope audit missing or ambiguous")
	}
	var value squarespaceLegacyScopeEvidence
	if json.Unmarshal([]byte(rows[0].Detail), &value) != nil || value.LocalOrderID != local.ID || value.UserID != local.UserID || value.ExternalOrderID != ledger.ExternalOrderID || value.WebsiteID != ledger.WebsiteID || value.CheckoutReference != local.OutTradeNo || value.QuoteHash != hash || value.OrderScopeMode != provider.SquarespaceScopeDedicatedSiteService || value.Purpose != provider.SquarespaceBalanceTopupPurpose || value.ExpectedServiceName != provider.SquarespaceExpectedServiceName || !value.OTPVerified || value.OTPPurpose != squarespaceClaimPurpose {
		return nil, errors.New("legacy scope audit tuple changed")
	}
	return &value, nil
}
func (s *PaymentService) validateSquarespaceReceiptClaim(ctx context.Context, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, docs []provider.SquarespaceTransactionDocument) error {
	grant, err := s.squarespaceReceiptScope(ctx, local, remote, nil)
	if err != nil {
		return err
	}
	_, err = verifiedSquarespaceReceiptClaimProofWithScope(local, remote, docs, grant.Config)
	return err
}
func (s *PaymentService) validateSquarespaceNotificationScope(ctx context.Context, local *dbent.PaymentOrder, tradeNo string, metadata map[string]string) error {
	if metadata["assisted_legacy_scope"] != "true" {
		return validateSquarespaceRetailMetadata(local, metadata)
	}
	ledger, err := s.entClient.PaymentExternalOrder.Query().Where(paymentexternalorder.ProviderKeyEQ("squarespace"), paymentexternalorder.LocalOrderIDEQ(local.ID), paymentexternalorder.ExternalOrderIDEQ(tradeNo), paymentexternalorder.BindingMethodEQ(squarespaceLegacyScopeBinding)).Only(ctx)
	if err != nil {
		return errors.New("assisted scope has no trusted binding")
	}
	evidence, err := s.loadSquarespaceLegacyScopeEvidence(ctx, local, ledger)
	if err != nil {
		return err
	}
	if metadata["website_id"] != evidence.WebsiteID || metadata["currency"] != "GBP" || metadata["checkout_reference"] != local.OutTradeNo || metadata["order_scope_mode"] != evidence.OrderScopeMode || metadata["payment_purpose"] != evidence.Purpose || metadata["expected_service_name"] != evidence.ExpectedServiceName || metadata["actual_product_id"] != evidence.ActualProductID {
		return errors.New("assisted legacy payment metadata changed")
	}
	return ValidateSquarespaceRetailPaymentTimes(PaymentOrderRetailQuote(local), metadata)
}
func squarespaceScopeMetadata(grant *squarespaceScopeGrant) map[string]string {
	cfg := grant.Config
	meta := map[string]string{"order_scope_mode": cfg["orderScopeMode"]}
	if cfg["orderScopeMode"] == provider.SquarespaceScopeFixedProduct {
		meta["product_id"] = cfg["productId"]
	} else {
		meta["payment_purpose"] = cfg["paymentPurpose"]
		meta["expected_service_name"] = cfg["expectedServiceName"]
		if grant.ActualProductID != "" {
			meta["actual_product_id"] = grant.ActualProductID
		}
	}
	if grant.AssistedLegacy {
		meta["assisted_legacy_scope"] = "true"
	}
	return meta
}
func (s *PaymentService) storeSquarespaceLegacyScopeAudit(ctx context.Context, client *dbent.Client, local *dbent.PaymentOrder, remote *provider.SquarespaceOrder, grant *squarespaceScopeGrant, quoteHash string) error {
	value := squarespaceLegacyScopeEvidence{LocalOrderID: local.ID, UserID: local.UserID, ExternalOrderID: remote.ID, WebsiteID: grant.Config["websiteId"], CheckoutReference: local.OutTradeNo, QuoteHash: quoteHash, ActualProductID: grant.ActualProductID, OrderScopeMode: grant.Config["orderScopeMode"], ExpectedServiceName: grant.Config["expectedServiceName"], Purpose: grant.Config["paymentPurpose"]}
	if err := validateSquarespaceReceiptAuthority(ctx, local, remote); err != nil {
		return err
	}
	authority, ok := ctx.Value(squarespaceVerifiedReceiptClaimKey{}).(squarespaceVerifiedReceiptClaimAuthority)
	if !ok {
		return errors.New("payer OTP authority missing or changed")
	}
	value.OTPVerified = true
	value.OTPPurpose = squarespaceClaimPurpose
	value.PayerEmailHash = authority.payerEmailHash
	value.AuthEmailHash = authority.authEmailHash
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(local.ID, 10)).SetAction(squarespaceLegacyScopeAudit).SetOperator("merchant-review+receipt-otp").SetDetail(string(encoded)).Save(ctx)
	return err
}
