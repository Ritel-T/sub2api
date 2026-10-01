//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"entgo.io/ent"
	"errors"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/stretchr/testify/require"
)

func squarespaceDedicatedTestShape(remote *provider.SquarespaceOrder) {
	zero := provider.SquarespaceMoney{Currency: "GBP", Value: "0.00"}
	remote.Subtotal = remote.GrandTotal
	remote.ShippingTotal = zero
	remote.TaxTotal = zero
	remote.DiscountTotal = zero
	qty := 1
	remote.LineItems = []provider.SquarespaceLineItem{{ID: "line_1", ProductID: "0123456789abcdef01234567", LineItemType: "SERVICE", ProductName: "Pay", Quantity: &qty, UnitPricePaid: remote.GrandTotal, VariantIsNull: true, SKUIsNull: true, CustomizationsEmpty: true}}
}
func squarespaceReviewedLegacyFixture(t *testing.T) (*SquarespacePaymentBridge, *dbent.PaymentOrder, *provider.SquarespaceOrder, []provider.SquarespaceTransactionDocument, map[string]string) {
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	ctx := context.Background()
	q := PaymentOrderRetailQuote(local)
	q.PaymentClaimMode = "receipt_otp"
	local.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(q)
	var err error
	local, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetProviderSnapshot(local.ProviderSnapshot).Save(ctx)
	require.NoError(t, err)
	squarespaceDedicatedTestShape(remote)
	remote.TopUpReference = ""
	remote.ReferenceStatus = "missing"
	config := map[string]string{"websiteId": "site_1", "productId": q.ProductID, "currency": "GBP", "paymentClaimMode": "receipt_otp", "payLinkUrl": "https://test.squarespace.com/pay", "orderScopeMode": provider.SquarespaceScopeDedicatedSiteService, "paymentPurpose": provider.SquarespaceBalanceTopupPurpose, "expectedServiceName": provider.SquarespaceExpectedServiceName, "reviewedLocalOrderId": strconv.FormatInt(local.ID, 10), "reviewedUserId": strconv.FormatInt(local.UserID, 10), "reviewedExternalOrderId": remote.ID, "reviewedCheckoutReference": local.OutTradeNo}
	instID, _ := strconv.ParseInt(*local.ProviderInstanceID, 10, 64)
	encoded, _ := json.Marshal(config)
	_, err = bridge.client.PaymentProviderInstance.UpdateOneID(instID).SetConfig(string(encoded)).Save(ctx)
	require.NoError(t, err)
	settings := bridge.config.settingRepo.(*paymentConfigSettingRepoStub)
	settings.values[SettingPaymentEnabled] = "false"
	ids, _ := json.Marshal([]int64{local.UserID})
	settings.values[SettingMerchantTestUserIDs] = string(ids)
	return bridge, local, remote, docs, config
}
func TestSquarespaceDedicatedSnapshotAcceptsDynamicProductsOnlyWithinExplicitScope(t *testing.T) {
	_, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	q := PaymentOrderRetailQuote(local)
	q.ProductID = ""
	q.OrderScopeMode = provider.SquarespaceScopeDedicatedSiteService
	q.ExpectedServiceName = provider.SquarespaceExpectedServiceName
	q.Purpose = provider.SquarespaceBalanceTopupPurpose
	local.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(q)
	delete(local.ProviderSnapshot, "product_id")
	local.ProviderSnapshot["order_scope_mode"] = q.OrderScopeMode
	local.ProviderSnapshot["expected_service_name"] = q.ExpectedServiceName
	local.ProviderSnapshot["purpose"] = q.Purpose
	squarespaceDedicatedTestShape(remote)
	require.Empty(t, validateSquarespaceFrozenTopupProduct(local, remote))
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	require.Empty(t, validateSquarespaceQuoteFinancialProof(local, proof, "site_1"))
	remote.LineItems[0].ProductID = "abcdef0123456789abcdef01"
	require.Empty(t, validateSquarespaceFrozenTopupProduct(local, remote))
	remote.LineItems[0].ProductName = "other service"
	require.NotEmpty(t, validateSquarespaceFrozenTopupProduct(local, remote))
	remote.LineItems[0].ProductName = "Pay"
	local.ProviderSnapshot["purpose"] = "other-sale"
	require.NotEmpty(t, validateSquarespaceFrozenTopupProduct(local, remote))
}
func TestSquarespaceReviewedLegacyScopeIsExactAndPreservesOriginalQuote(t *testing.T) {
	bridge, local, remote, docs, config := squarespaceReviewedLegacyFixture(t)
	ctx := context.Background()
	before, err := json.Marshal(local.ProviderSnapshot)
	require.NoError(t, err)
	oldHash, err := SquarespaceReceiptQuoteHash(local)
	require.NoError(t, err)
	require.NoError(t, bridge.payment.validateSquarespaceReceiptClaim(ctx, local, remote, docs))
	grant, err := bridge.payment.squarespaceReceiptScope(ctx, local, remote, nil)
	require.NoError(t, err)
	require.True(t, grant.AssistedLegacy)
	for _, key := range []string{"reviewedLocalOrderId", "reviewedUserId", "reviewedExternalOrderId", "reviewedCheckoutReference", "websiteId"} {
		t.Run(key, func(t *testing.T) {
			modified := map[string]string{}
			for k, v := range config {
				modified[k] = v
			}
			modified[key] = "mismatch"
			encoded, _ := json.Marshal(modified)
			id, _ := strconv.ParseInt(*local.ProviderInstanceID, 10, 64)
			_, err := bridge.client.PaymentProviderInstance.UpdateOneID(id).SetConfig(string(encoded)).Save(ctx)
			require.NoError(t, err)
			require.Error(t, bridge.payment.validateSquarespaceReceiptClaim(ctx, local, remote, docs))
			encoded, _ = json.Marshal(config)
			_, err = bridge.client.PaymentProviderInstance.UpdateOneID(id).SetConfig(string(encoded)).Save(ctx)
			require.NoError(t, err)
		})
	}
	bridge.config.settingRepo.(*paymentConfigSettingRepoStub).values[SettingMerchantTestUserIDs] = "[]"
	require.Error(t, bridge.payment.validateSquarespaceReceiptClaim(ctx, local, remote, docs))
	ids, _ := json.Marshal([]int64{local.UserID})
	bridge.config.settingRepo.(*paymentConfigSettingRepoStub).values[SettingMerchantTestUserIDs] = string(ids)
	after, err := json.Marshal(local.ProviderSnapshot)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
	newHash, err := SquarespaceReceiptQuoteHash(local)
	require.NoError(t, err)
	require.Equal(t, oldHash, newHash)
}
func TestSquarespaceReviewedLegacyBindingPersistsEvidenceForRecoveryAndRefund(t *testing.T) {
	bridge, local, remote, docs, config := squarespaceReviewedLegacyFixture(t)
	ctx := context.Background()
	hash, err := SquarespaceReceiptQuoteHash(local)
	require.NoError(t, err)
	authority := withVerifiedSquarespaceReceiptClaim(ctx, local.UserID, local.ID, remote.ID, hash)
	_, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(OrderStatusPending).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, bridge.payment.BindVerifiedReceiptClaim(authority, local, remote, docs))
	ledger, err := bridge.client.PaymentExternalOrder.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, squarespaceLegacyScopeBinding, ledger.BindingMethod)
	require.Equal(t, hash, ledger.QuoteHash)
	evidence, err := bridge.payment.loadSquarespaceLegacyScopeEvidence(ctx, local, ledger)
	require.NoError(t, err)
	require.Equal(t, remote.LineItems[0].ProductID, evidence.ActualProductID)
	for _, key := range []string{"reviewedLocalOrderId", "reviewedUserId", "reviewedExternalOrderId", "reviewedCheckoutReference"} {
		delete(config, key)
	}
	encoded, _ := json.Marshal(config)
	id, _ := strconv.ParseInt(*local.ProviderInstanceID, 10, 64)
	_, err = bridge.client.PaymentProviderInstance.UpdateOneID(id).SetConfig(string(encoded)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, bridge.processExternalOrder(ctx, mustSquarespaceTestInstance(t, bridge, local), "site_1", remote, docs, ledger))
	scope, err := bridge.payment.squarespaceReceiptScope(ctx, local, remote, ledger)
	require.NoError(t, err)
	require.True(t, scope.AssistedLegacy)
	require.NoError(t, provider.ValidateSquarespaceOrderScope(remote, scope.Config))
}
func mustSquarespaceTestInstance(t *testing.T, bridge *SquarespacePaymentBridge, local *dbent.PaymentOrder) *dbent.PaymentProviderInstance {
	t.Helper()
	id, err := strconv.ParseInt(*local.ProviderInstanceID, 10, 64)
	require.NoError(t, err)
	inst, err := bridge.client.PaymentProviderInstance.Get(context.Background(), id)
	require.NoError(t, err)
	return inst
}
func TestSquarespaceOldQuoteHashDoesNotGainScopeFields(t *testing.T) {
	old := `{"credited_amount_usd":1,"base_amount_gbp":0.76,"included_cost_gbp":0.3,"total_amount_gbp":1.06,"pay_amount":1.06,"currency":"GBP","fx":{"GBP":1,"USD":1.324692596654145,"CNY":8.879931869618983},"fx_source":"ecb","fx_asof":"2026-09-29T00:00:00Z","issued_at":"2026-09-30T06:41:54Z","expires_at":"2026-09-30T07:41:54Z","checkout_reference":"sub2_0e2bb651d1b046e5a70f2e113240bea5","cost_rate":4,"fixed_cost_gbp":0.25,"payment_claim_mode":"receipt_otp","product_id":"6abc2d02ac3ba7447cdc0752"}`
	var mapValue map[string]any
	require.NoError(t, json.Unmarshal([]byte(old), &mapValue))
	order := &dbent.PaymentOrder{ProviderSnapshot: map[string]any{"retail_quote": mapValue}}
	quote := PaymentOrderRetailQuote(order)
	encoded, err := json.Marshal(quote)
	require.NoError(t, err)
	var legacy struct {
		CreditedAmountUSD float64            `json:"credited_amount_usd"`
		BaseAmountGBP     float64            `json:"base_amount_gbp"`
		IncludedCostGBP   float64            `json:"included_cost_gbp"`
		TotalAmountGBP    float64            `json:"total_amount_gbp"`
		PayAmount         float64            `json:"pay_amount"`
		Currency          string             `json:"currency"`
		FX                map[string]float64 `json:"fx"`
		FXSource          string             `json:"fx_source"`
		FXAsOf            json.RawMessage    `json:"fx_asof"`
		IssuedAt          json.RawMessage    `json:"issued_at"`
		ExpiresAt         json.RawMessage    `json:"expires_at"`
		CheckoutReference string             `json:"checkout_reference"`
		CostRate          float64            `json:"cost_rate"`
		FixedCostGBP      float64            `json:"fixed_cost_gbp"`
		PaymentClaimMode  string             `json:"payment_claim_mode,omitempty"`
		ProductID         string             `json:"product_id,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(old), &legacy))
	before, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(encoded))
	require.Equal(t, string(before), string(encoded))
	sum := sha256.Sum256(before)
	expected := hex.EncodeToString(sum[:])
	hash, err := SquarespaceReceiptQuoteHash(order)
	require.NoError(t, err)
	require.Equal(t, expected, hash)
}

func TestSquarespaceReceiptCannotBeAutomaticallyBoundByReference(t *testing.T) {
	ctx := context.Background()
	bridge, local, remote, docs, _ := squarespaceBridgeDBFixture(t)
	q := PaymentOrderRetailQuote(local)
	q.PaymentClaimMode = "receipt_otp"
	local.ProviderSnapshot["retail_quote"] = retailQuoteSnapshot(q)
	var err error
	local, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(OrderStatusPending).SetProviderSnapshot(local.ProviderSnapshot).Save(ctx)
	require.NoError(t, err)
	ledger, err := bridge.observeOrder(ctx, "site_1", remote)
	require.NoError(t, err)
	proof, code := verifySquarespaceProof(remote, docs)
	require.Empty(t, code)
	_, err = bridge.bindExternalOrder(ctx, ledger, local, remote, proof, "reference")
	require.Error(t, err)
	require.NoError(t, bridge.processExternalOrder(ctx, mustSquarespaceTestInstance(t, bridge, local), "site_1", remote, docs, ledger))
	ledger, err = bridge.client.PaymentExternalOrder.Get(ctx, ledger.ID)
	require.NoError(t, err)
	require.Nil(t, ledger.LocalOrderID)
	require.Equal(t, "PAYER_OTP_REQUIRED", ledger.AnomalyCode)
	count, err := bridge.client.PaymentExternalPayment.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
func TestSquarespaceFreshReceiptBindingRejectsCancellationAfterOTP(t *testing.T) {
	ctx := context.Background()
	bridge, local, remote, docs, _ := squarespaceReviewedLegacyFixture(t)
	var err error
	local, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(OrderStatusPending).Save(ctx)
	require.NoError(t, err)
	hash, err := SquarespaceReceiptQuoteHash(local)
	require.NoError(t, err)
	authority := withVerifiedSquarespaceReceiptClaim(ctx, local.UserID, local.ID, remote.ID, hash)
	ledger, err := bridge.observeOrder(ctx, "site_1", remote)
	require.NoError(t, err)
	grant, err := bridge.payment.squarespaceReceiptScope(ctx, local, remote, nil)
	require.NoError(t, err)
	proof, err := verifiedSquarespaceReceiptClaimProofWithScope(local, remote, docs, grant.Config)
	require.NoError(t, err)
	_, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(OrderStatusCancelled).Save(ctx)
	require.NoError(t, err)
	_, err = bridge.bindExternalOrder(authority, ledger, local, remote, proof, squarespaceLegacyScopeBinding)
	require.Error(t, err)
	ledger, err = bridge.client.PaymentExternalOrder.Get(ctx, ledger.ID)
	require.NoError(t, err)
	require.Nil(t, ledger.LocalOrderID)
	count, err := bridge.client.PaymentAuditLog.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
func TestSquarespaceLegacyReviewAuditFailureRollsBackFreshBinding(t *testing.T) {
	ctx := context.Background()
	bridge, local, remote, docs, _ := squarespaceReviewedLegacyFixture(t)
	var err error
	local, err = bridge.client.PaymentOrder.UpdateOneID(local.ID).SetStatus(OrderStatusPending).Save(ctx)
	require.NoError(t, err)
	hash, err := SquarespaceReceiptQuoteHash(local)
	require.NoError(t, err)
	authority := withVerifiedSquarespaceReceiptClaim(ctx, local.UserID, local.ID, remote.ID, hash)
	ledger, err := bridge.observeOrder(ctx, "site_1", remote)
	require.NoError(t, err)
	grant, err := bridge.payment.squarespaceReceiptScope(ctx, local, remote, nil)
	require.NoError(t, err)
	proof, err := verifiedSquarespaceReceiptClaimProofWithScope(local, remote, docs, grant.Config)
	require.NoError(t, err)
	bridge.client.PaymentAuditLog.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) {
			return nil, errors.New("controlled audit failure")
		})
	})
	_, err = bridge.bindExternalOrder(authority, ledger, local, remote, proof, squarespaceLegacyScopeBinding)
	require.Error(t, err)
	ledger, err = bridge.client.PaymentExternalOrder.Get(ctx, ledger.ID)
	require.NoError(t, err)
	require.Nil(t, ledger.LocalOrderID)
	count, err := bridge.client.PaymentExternalPayment.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
