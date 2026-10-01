//go:build integration

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalpayment"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalrefundjournal"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type legacyScopePGSettings struct {
	SettingRepository
	mu     sync.RWMutex
	values map[string]string
}

func (s *legacyScopePGSettings) GetValue(_ context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values[key], nil
}
func (s *legacyScopePGSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = s.values[key]
	}
	return result, nil
}
func (s *legacyScopePGSettings) merchant(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[SettingMerchantTestUserIDs] = "[]"
	if id > 0 {
		s.values[SettingMerchantTestUserIDs] = fmt.Sprintf("[%d]", id)
	}
}

type legacyScopePGData struct {
	*squarespacePGFixtureData
	settings                                                 *legacyScopePGSettings
	approved                                                 map[string]string
	quoteHash, productID, instanceID, snapshotSHA, reference string
}

func legacyScopePGSnapshotSHA(t *testing.T, local *dbent.PaymentOrder) string {
	t.Helper()
	encoded, err := json.Marshal(local.ProviderSnapshot)
	require.NoError(t, err)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func legacyScopePGSetConfig(t *testing.T, ctx context.Context, f *legacyScopePGData, cfg map[string]string) {
	t.Helper()
	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	f.instance, err = f.bridge.client.PaymentProviderInstance.UpdateOneID(f.instance.ID).SetConfig(string(encoded)).Save(ctx)
	require.NoError(t, err)
}
func legacyScopePGMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func legacyScopePGFixture(t *testing.T, ctx context.Context, client *dbent.Client, deductSQL string, sequence int64, balance float64) *legacyScopePGData {
	t.Helper()
	raw := squarespacePGFixture(t, ctx, client, deductSQL, sequence, OrderStatusPending, balance)
	quote := *PaymentOrderRetailQuote(raw.local)
	quote.PaymentClaimMode = "receipt_otp"
	quote.OrderScopeMode, quote.ExpectedServiceName, quote.Purpose = "", "", ""
	encoded, err := json.Marshal(quote)
	require.NoError(t, err)
	var quoteMap map[string]any
	require.NoError(t, json.Unmarshal(encoded, &quoteMap))
	require.NotContains(t, quoteMap, "order_scope_mode", "legacy quote must not acquire the new scope fields")
	require.NotContains(t, quoteMap, "expected_service_name")
	require.NotContains(t, quoteMap, "purpose")
	snapshot := raw.local.ProviderSnapshot
	snapshot["retail_quote"] = quoteMap
	delete(snapshot, "order_scope_mode")
	delete(snapshot, "expected_service_name")
	delete(snapshot, "purpose")
	raw.local, err = client.PaymentOrder.UpdateOneID(raw.local.ID).SetProviderSnapshot(snapshot).Save(ctx)
	require.NoError(t, err)
	// Fixture-only dedicated Pay service intentionally has a different product
	// id from the old immutable fixed-SKU invoice, like the reviewed old quote.
	actualPID := fmt.Sprintf("%024x", sequence+9000)
	raw.remote.ID = fmt.Sprintf("%024x", sequence+7000)
	raw.remote.TopUpReference, raw.remote.ReferenceStatus = "", "missing"
	raw.remote.CustomerEmail = "fixture-payer@example.test"
	money := func(value string) provider.SquarespaceMoney {
		return provider.SquarespaceMoney{Currency: "GBP", Value: value}
	}
	raw.remote.Subtotal = money("10.00")
	raw.remote.ShippingTotal, raw.remote.TaxTotal, raw.remote.DiscountTotal = money("0.00"), money("0.00"), money("0.00")
	quantity := 1
	raw.remote.LineItems = []provider.SquarespaceLineItem{{ID: "fixture-service-line", ProductID: actualPID,
		LineItemType: "SERVICE", ProductName: provider.SquarespaceExpectedServiceName, Quantity: &quantity,
		UnitPricePaid: money("10.00"), VariantIsNull: true, SKUIsNull: true, CustomizationsEmpty: true}}
	raw.docs[0].SalesOrderID = raw.remote.ID
	proof, reason := verifySquarespaceProof(raw.remote, raw.docs)
	require.Empty(t, reason)
	raw.proof = proof
	settings := &legacyScopePGSettings{values: map[string]string{SettingPaymentEnabled: "false", SettingEnabledPaymentTypes: "squarespace", SettingBalancePayDisabled: "false"}}
	settings.merchant(raw.local.UserID)
	raw.bridge.config.settingRepo = settings
	hash, err := SquarespaceReceiptQuoteHash(raw.local)
	require.NoError(t, err)
	f := &legacyScopePGData{squarespacePGFixtureData: raw, settings: settings, quoteHash: hash,
		productID: PaymentOrderSquarespaceProductID(raw.local), instanceID: *raw.local.ProviderInstanceID,
		snapshotSHA: legacyScopePGSnapshotSHA(t, raw.local), reference: raw.local.OutTradeNo}
	f.approved = map[string]string{"websiteId": "site_1", "productId": f.productID,
		"referenceFieldLabel": provider.SquarespaceReferenceFieldLabel, "payLinkUrl": "https://fixture.squarespace.com/pay-link/",
		"currency": "GBP", "productionApproved": "false", "orderScopeMode": provider.SquarespaceScopeDedicatedSiteService,
		"paymentPurpose": provider.SquarespaceBalanceTopupPurpose, "expectedServiceName": provider.SquarespaceExpectedServiceName,
		"reviewedLocalOrderId": strconv.FormatInt(f.local.ID, 10), "reviewedUserId": strconv.FormatInt(f.local.UserID, 10),
		"reviewedExternalOrderId": f.remote.ID, "reviewedCheckoutReference": f.reference}
	legacyScopePGSetConfig(t, ctx, f, f.approved)
	require.NotEqual(t, f.productID, actualPID)
	require.NoError(t, provider.ValidateSquarespaceOrderScope(f.remote, f.approved))
	f.bridge.sourceFactory = func(context.Context, map[string]string) (squarespaceBridgeSource, error) {
		return &squarespacePGSource{order: f.remote, documents: f.docs}, nil
	}
	return f
}

func legacyScopePGAuthority(ctx context.Context, f *legacyScopePGData) context.Context {
	// PG coverage begins after the claim service's atomic OTP consume. Email
	// targeting, Lua/Redis consume and public challenge endpoints have separate
	// unit coverage; no SMTP, Square API or production account is used here.
	return withVerifiedSquarespaceReceiptClaim(ctx, f.local.UserID, f.local.ID, f.remote.ID, f.quoteHash)
}
func legacyScopePGImmutable(t *testing.T, ctx context.Context, f *legacyScopePGData) *dbent.PaymentOrder {
	t.Helper()
	current, err := f.bridge.client.PaymentOrder.Get(ctx, f.local.ID)
	require.NoError(t, err)
	hash, err := SquarespaceReceiptQuoteHash(current)
	require.NoError(t, err)
	require.Equal(t, f.quoteHash, hash)
	require.Equal(t, f.productID, PaymentOrderSquarespaceProductID(current))
	require.Equal(t, f.instanceID, *current.ProviderInstanceID)
	require.Equal(t, f.reference, current.OutTradeNo)
	require.Equal(t, f.snapshotSHA, legacyScopePGSnapshotSHA(t, current))
	return current
}
func legacyScopePGReviewAudit(t *testing.T, ctx context.Context, f *legacyScopePGData, ledger *dbent.PaymentExternalOrder) {
	t.Helper()
	rows, err := f.bridge.client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(f.local.ID, 10)), paymentauditlog.ActionEQ(squarespaceLegacyScopeAudit)).All(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	var evidence squarespaceLegacyScopeEvidence
	require.NoError(t, json.Unmarshal([]byte(rows[0].Detail), &evidence))
	require.Equal(t, f.local.ID, evidence.LocalOrderID)
	require.Equal(t, f.local.UserID, evidence.UserID)
	require.Equal(t, f.remote.ID, evidence.ExternalOrderID)
	require.Equal(t, "site_1", evidence.WebsiteID)
	require.Equal(t, f.reference, evidence.CheckoutReference)
	require.Equal(t, f.quoteHash, evidence.QuoteHash)
	require.Equal(t, strings.ToLower(f.remote.LineItems[0].ProductID), evidence.ActualProductID)
	require.Equal(t, provider.SquarespaceScopeDedicatedSiteService, evidence.OrderScopeMode)
	require.Equal(t, provider.SquarespaceBalanceTopupPurpose, evidence.Purpose)
	require.Equal(t, provider.SquarespaceExpectedServiceName, evidence.ExpectedServiceName)
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(rows[0].Detail), &fields))
	require.Equal(t, true, fields["otp_verified"])
	require.Equal(t, squarespaceClaimPurpose, fields["otp_purpose"])
	require.NotContains(t, rows[0].Detail, f.remote.CustomerEmail)
	require.NotContains(t, fields, "otp_code")
	require.NotContains(t, fields, "payer_email")
	_, err = f.bridge.payment.loadSquarespaceLegacyScopeEvidence(ctx, f.local, ledger)
	require.NoError(t, err)
}

func TestSquarespaceLegacyScopePostgres(t *testing.T) {
	cfg, db := squarespaceOAuthPGFixture(t)
	client := cfg.entClient
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Match the existing external-ledger PG fixture: use migration 262's real
	// PostgreSQL constraints, rather than only ent-generated SQLite schemas.
	_, err := db.ExecContext(ctx, "DROP TABLE payment_external_refund_journals, payment_external_payments, payment_sync_states, payment_external_orders")
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "262_squarespace_external_ledger.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	deductSQL := squarespacePGSourceSQL(t, "user_repo.go", "DeductAvailableBalance", "FOR UPDATE")

	t.Run("exact_merchant_review_OTP_and_atomic_audit_credit_once", func(t *testing.T) {
		f := legacyScopePGFixture(t, ctx, client, deductSQL, 101, 0)
		authority := legacyScopePGAuthority(ctx, f)
		require.Error(t, f.bridge.payment.BindVerifiedReceiptClaim(ctx, f.local, f.remote, f.docs), "no OTP authority must be rejected")
		wrongAuthority := withVerifiedSquarespaceReceiptClaim(ctx, f.local.UserID+1, f.local.ID, f.remote.ID, f.quoteHash)
		require.Error(t, f.bridge.payment.BindVerifiedReceiptClaim(wrongAuthority, f.local, f.remote, f.docs))
		f.settings.merchant(0)
		require.Error(t, f.bridge.payment.BindVerifiedReceiptClaim(authority, f.local, f.remote, f.docs), "a review is not a merchant UID grant")
		f.settings.merchant(f.local.UserID)
		wrong := map[string]string{"reviewedLocalOrderId": strconv.FormatInt(f.local.ID+1, 10),
			"reviewedUserId": strconv.FormatInt(f.local.UserID+1, 10), "reviewedExternalOrderId": fmt.Sprintf("%024x", 99999),
			"reviewedCheckoutReference": "sub2_" + strings.Repeat("f", 32), "websiteId": "other_site"}
		for key, value := range wrong {
			changed := legacyScopePGMap(f.approved)
			changed[key] = value
			legacyScopePGSetConfig(t, ctx, f, changed)
			require.Error(t, f.bridge.payment.BindVerifiedReceiptClaim(authority, f.local, f.remote, f.docs), "mismatched review key: %s", key)
			legacyScopePGImmutable(t, ctx, f)
		}
		legacyScopePGSetConfig(t, ctx, f, f.approved)
		grant, err := f.bridge.payment.squarespaceReceiptScope(ctx, f.local, f.remote, nil)
		require.NoError(t, err)
		require.True(t, grant.AssistedLegacy)
		ledger, err := f.bridge.observeOrder(ctx, "site_1", f.remote)
		require.NoError(t, err)
		_, err = f.bridge.bindExternalOrder(ctx, ledger, f.local, f.remote, f.proof, squarespaceLegacyScopeBinding)
		require.Error(t, err, "even the private legacy binder requires OTP authority")
		_, err = f.bridge.bindExternalOrder(authority, ledger, f.local, f.remote, f.proof, "reference")
		require.Error(t, err, "receipt invoice must not bind through a background reference path")
		_, err = client.PaymentOrder.UpdateOneID(f.local.ID).SetStatus("cancelled").Save(ctx)
		require.NoError(t, err)
		require.Error(t, f.bridge.payment.BindVerifiedReceiptClaim(authority, f.local, f.remote, f.docs), "cancelled invoice must not acquire a fresh binding")
		_, err = client.PaymentOrder.UpdateOneID(f.local.ID).SetStatus(OrderStatusPending).Save(ctx)
		require.NoError(t, err)

		var rejectAudit atomic.Bool
		rejectAudit.Store(true)
		defer rejectAudit.Store(false)
		client.PaymentAuditLog.Use(func(next ent.Mutator) ent.Mutator {
			return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
				if audit, ok := mutation.(*dbent.PaymentAuditLogMutation); ok {
					action, _ := audit.Action()
					if rejectAudit.Load() && action == squarespaceLegacyScopeAudit {
						return nil, errors.New("fixture legacy scope audit rejected")
					}
				}
				return next.Mutate(ctx, mutation)
			})
		})
		err = f.bridge.payment.BindVerifiedReceiptClaim(authority, f.local, f.remote, f.docs)
		require.ErrorContains(t, err, "fixture legacy scope audit rejected")
		unbound, err := client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
		require.Nil(t, unbound.LocalOrderID)
		require.Equal(t, ledger.BindingMethod, unbound.BindingMethod)
		count, err := client.PaymentExternalPayment.Query().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(ledger.ID)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		count, err = client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(f.local.ID, 10)), paymentauditlog.ActionEQ(squarespaceLegacyScopeAudit)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		owner, err := client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Zero(t, owner.Balance)
		require.Zero(t, owner.TotalRecharged)
		count, err = client.RedeemCode.Query().Where(redeemcode.CodeEQ(f.local.RechargeCode)).Count(ctx)
		require.NoError(t, err)
		require.Zero(t, count)
		legacyScopePGImmutable(t, ctx, f)
		rejectAudit.Store(false)
		start, outcomes := make(chan struct{}), make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				outcomes <- f.bridge.payment.BindVerifiedReceiptClaim(authority, f.local, f.remote, f.docs)
			}()
		}
		close(start)
		for range 2 {
			select {
			case err = <-outcomes:
				if err != nil {
					require.Equal(t, "CONFLICT", infraerrors.Reason(err))
				}
			case <-ctx.Done():
				t.Fatal("concurrent reviewed claims did not finish")
			}
		}
		require.NoError(t, f.bridge.payment.BindVerifiedReceiptClaim(authority, f.local, f.remote, f.docs), "replay must converge without a second credit")
		bound, err := client.PaymentExternalOrder.Get(ctx, ledger.ID)
		require.NoError(t, err)
		require.Equal(t, squarespaceLegacyScopeBinding, bound.BindingMethod)
		require.Equal(t, f.local.ID, *bound.LocalOrderID)
		require.Equal(t, "CREDITED", bound.Status)
		legacyScopePGReviewAudit(t, ctx, f, bound)
		owner, err = client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 20.0, owner.Balance)
		require.Equal(t, 20.0, owner.TotalRecharged)
		require.Equal(t, 7.0, owner.FrozenBalance)
		used, err := client.RedeemCode.Query().Where(redeemcode.CodeEQ(f.local.RechargeCode)).Only(ctx)
		require.NoError(t, err)
		require.Equal(t, StatusUsed, used.Status)
		require.Equal(t, f.local.UserID, *used.UsedBy)
		count, err = client.PaymentExternalPayment.Query().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(ledger.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		_, err = client.PaymentExternalOrder.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").SetExternalOrderID(f.remote.ID).Save(ctx)
		require.True(t, dbent.IsConstraintError(err), "real PostgreSQL external-order uniqueness is required")
		_, err = client.PaymentExternalPayment.Create().SetProviderKey("squarespace").SetWebsiteID("site_1").SetPaymentID(f.proof.PaymentID).SetExternalOrderLedgerID(bound.ID).SetCurrency("GBP").SetAmountMinor(1000).SetPaidAt(f.proof.PaidAt).Save(ctx)
		require.True(t, dbent.IsConstraintError(err), "real PostgreSQL payment uniqueness is required")
		require.Equal(t, OrderStatusCompleted, legacyScopePGImmutable(t, ctx, f).Status)
		t.Log("PG exact review/merchant/OTP guards, atomic audit rollback, unique binding and one credit verified")
	})

	t.Run("withdraw_review_recover_from_audit_and_refund_original_credit", func(t *testing.T) {
		f := legacyScopePGFixture(t, ctx, client, deductSQL, 102, 5)
		authority := legacyScopePGAuthority(ctx, f)
		ledger, err := f.bridge.observeOrder(ctx, "site_1", f.remote)
		require.NoError(t, err)
		bound, err := f.bridge.bindExternalOrder(authority, ledger, f.local, f.remote, f.proof, squarespaceLegacyScopeBinding)
		require.NoError(t, err)
		require.Equal(t, "BOUND", bound.Status)
		legacyScopePGReviewAudit(t, ctx, f, bound)
		// This is a post-OTP binding/crash checkpoint, before actual fulfillment.
		owner, err := client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 5.0, owner.Balance)
		require.Zero(t, owner.TotalRecharged)
		withdrawn := legacyScopePGMap(f.approved)
		for _, key := range []string{"reviewedLocalOrderId", "reviewedUserId", "reviewedExternalOrderId", "reviewedCheckoutReference"} {
			delete(withdrawn, key)
		}
		legacyScopePGSetConfig(t, ctx, f, withdrawn)
		_, err = f.bridge.payment.squarespaceReceiptScope(ctx, f.local, f.remote, nil)
		require.Error(t, err, "withdrawn current review must not authorize a fresh bind")
		_, err = f.bridge.payment.squarespaceReceiptScope(ctx, f.local, f.remote, bound)
		require.NoError(t, err, "bound recovery must use the persisted exact scope audit")
		fakeSource := &squarespacePGSource{order: f.remote, documents: f.docs}
		require.NoError(t, f.bridge.recoverBoundOrders(ctx, f.instance, "site_1", fakeSource))
		require.NoError(t, f.bridge.recoverBoundOrders(ctx, f.instance, "site_1", fakeSource))
		bound, err = client.PaymentExternalOrder.Get(ctx, bound.ID)
		require.NoError(t, err)
		require.Equal(t, "CREDITED", bound.Status)
		owner, err = client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 25.0, owner.Balance)
		require.Equal(t, 20.0, owner.TotalRecharged)
		legacyScopePGReviewAudit(t, ctx, f, bound)
		legacyScopePGImmutable(t, ctx, f)

		// New collection is disabled entirely. The already-credited immutable
		// entitlement can still be refunded from its durable binding/audit.
		f.settings.merchant(0)
		f.instance, err = client.PaymentProviderInstance.UpdateOneID(f.instance.ID).SetEnabled(false).Save(ctx)
		require.NoError(t, err)
		money := func(value string) provider.SquarespaceMoney {
			return provider.SquarespaceMoney{Currency: "GBP", Value: value}
		}
		refundedOn := time.Now().UTC().Format(time.RFC3339Nano)
		f.remote.RefundedTotal = money("2.00")
		f.docs[0].TotalNetPayment = money("7.55")
		f.docs[0].Payments[0].NetAmount = money("7.55")
		f.docs[0].Payments[0].RefundedAmount = money("2.00")
		f.docs[0].Payments[0].Refunds = []provider.SquarespaceRefund{{ID: "fixture-legacy-partial-refund", RefundedOn: refundedOn, Amount: money("2.00")}}
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", f.remote, f.docs, bound))
		bound, err = client.PaymentExternalOrder.Get(ctx, bound.ID)
		require.NoError(t, err)
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", f.remote, f.docs, bound))
		owner, err = client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 21.0, owner.Balance)
		require.Equal(t, 7.0, owner.FrozenBalance)
		require.Equal(t, int64(200), bound.RefundedMinor)

		f.remote.PaymentState = "REFUNDED"
		f.remote.RefundedTotal = money("10.00")
		f.docs[0].TotalNetPayment = money("-0.45")
		f.docs[0].Payments[0].NetAmount = money("-0.45")
		f.docs[0].Payments[0].RefundedAmount = money("10.00")
		f.docs[0].Payments[0].Refunds = append(f.docs[0].Payments[0].Refunds, provider.SquarespaceRefund{ID: "fixture-legacy-final-refund", RefundedOn: refundedOn, Amount: money("8.00")})
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", f.remote, f.docs, bound))
		bound, err = client.PaymentExternalOrder.Get(ctx, bound.ID)
		require.NoError(t, err)
		require.NoError(t, f.bridge.processExternalOrder(ctx, f.instance, "site_1", f.remote, f.docs, bound))
		owner, err = client.User.Get(ctx, f.local.UserID)
		require.NoError(t, err)
		require.Equal(t, 5.0, owner.Balance, "full refund must restore original available balance")
		require.Equal(t, 7.0, owner.FrozenBalance, "preexisting frozen balance must remain intact")
		require.Equal(t, 20.0, owner.TotalRecharged, "only one original credit exists in historical accounting")
		require.Equal(t, "REFUNDED", bound.Status)
		require.Equal(t, int64(1000), bound.RefundedMinor)
		require.Equal(t, int64(2000000000), bound.RefundTargetUsdUnits)
		require.Equal(t, bound.RefundTargetUsdUnits, bound.RecoveredUsdUnits)
		require.Zero(t, bound.DebtUsdUnits)
		require.Equal(t, int64(2), f.users.deductionCalls.Load(), "each cumulative refund delta must be applied once")
		count, err := client.PaymentExternalRefundJournal.Query().Where(paymentexternalrefundjournal.ExternalOrderLedgerIDEQ(bound.ID)).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		count, err = client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(f.local.ID, 10)), paymentauditlog.ActionEQ("SQUARESPACE_EXTERNAL_REFUND")).Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		err = client.PaymentExternalRefundJournal.Create().SetExternalOrderLedgerID(bound.ID).SetCumulativeRefundMinor(1000).SetDeltaRefundMinor(800).SetTargetUsdUnits(2000000000).SetDeltaTargetUsdUnits(1600000000).Exec(ctx)
		require.True(t, dbent.IsConstraintError(err), "migration 262 must reject a duplicate cumulative refund journal")
		paid, err := client.PaymentExternalPayment.Query().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(bound.ID)).Only(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(1000), paid.RefundedMinor)
		require.Equal(t, OrderStatusRefunded, legacyScopePGImmutable(t, ctx, f).Status)
		legacyScopePGReviewAudit(t, ctx, f, bound)
		t.Log("PG durable scope recovery and proportional/full refund restored original entitlement without repricing")
	})
	_ = db // fixture cleanup is owned by the reused PG18 helper
}
