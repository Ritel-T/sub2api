package service

import (
	"context"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func (s *PaymentConfigService) validateGelvInstance(ctx context.Context, id int64, key, types string, refund bool) error {
	if key != payment.TypeGelv {
		return nil
	}
	if types != payment.TypeGelv || refund {
		return infraerrors.BadRequest("INVALID_GELV_CAPABILITY", "mini-program supports balance QR payments and channel manual refunds only")
	}
	count, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKeyEQ(payment.TypeGelv), paymentproviderinstance.EnabledEQ(true), paymentproviderinstance.IDNEQ(id)).Count(ctx)
	if err != nil {
		return err
	}
	if count != 0 {
		return infraerrors.Conflict("GELV_MULTIPLE_INSTANCES", "only one mini-program provider may be enabled")
	}
	return nil
}

// quoteGelvPayment keeps the shared mini-program's six fixed cash products.
// Site credit includes the separately configured fee, never GBP conversion.
func (s *PaymentService) quoteGelvPayment(ctx context.Context, req CreateOrderRequest, cfg *PaymentConfig) (*RetailQuoteResponse, error) {
	instances, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKeyEQ(payment.TypeGelv), paymentproviderinstance.EnabledEQ(true)).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(instances) != 1 {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel is not configured")
	}
	config, err := s.configService.decryptConfig(instances[0].Config)
	if err != nil {
		return nil, err
	}
	options, err := provider.GelvCreditOptions(config)
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("GELV_CREDITS_NOT_CONFIGURED", "mini-program credit amounts are not configured")
	}
	cash := 0.0
	for _, option := range options {
		if decimal.NewFromFloat(option.Credits).Equal(decimal.NewFromFloat(req.Amount)) {
			cash = option.CashCNY
			break
		}
	}
	if cash == 0 {
		return nil, infraerrors.BadRequest("GELV_DENOMINATION_REQUIRED", "choose one of the configured mini-program recharge tiers")
	}
	if (cfg.MinAmount > 0 && cash < cfg.MinAmount) || (cfg.MaxAmount > 0 && cash > cfg.MaxAmount) {
		return nil, infraerrors.BadRequest("INVALID_AMOUNT", "cash denomination is outside the configured range")
	}
	sel, err := s.selectCreateOrderInstance(ctx, req, cfg, cash)
	if err != nil {
		return nil, err
	}
	if sel.ProviderKey != payment.TypeGelv {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel changed")
	}
	if _, err = provider.NewGelv(sel.InstanceID, sel.Config); err != nil {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel is not configured")
	}
	now := time.Now().UTC().Truncate(time.Second)
	ttl := cfg.BalanceRetailQuoteTTLSeconds
	if ttl < 60 || ttl > 900 {
		ttl = 900
	}
	reference, err := newRetailCheckoutReference()
	if err != nil {
		return nil, err
	}
	minor, _ := payment.AmountToMinorUnit(payment.FormatAmountForCurrency(cash, "CNY"), "CNY")
	android, _ := provider.GelvCredits(minor, "wechat_virtual")
	ios, _ := provider.GelvCredits(minor, "apple_iap")
	quote := &RetailQuote{PricingMode: "gelv_channel_cny", CreditByChannel: map[string]float64{"wechat_virtual": android, "apple_iap": ios}, CreditedAmountUSD: req.Amount, PricingBasisCurrency: "CNY", BaseAmountCNY: req.Amount, PayAmount: cash, Currency: "CNY", FX: map[string]float64{}, FXSource: "fixed_cny", FXAsOf: now, IssuedAt: now, ExpiresAt: now.Add(time.Duration(ttl) * time.Second), CheckoutReference: reference}
	quote.ProductID, _ = provider.GelvProductForMinor(minor)
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: req.UserID, PaymentType: payment.TypeGelv, ProviderInstanceID: sel.InstanceID, ProviderKey: sel.ProviderKey, ProviderIdentity: buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{}), Quote: quote}
	token, err := s.signRetailQuote(claims)
	if err != nil {
		return nil, err
	}
	return &RetailQuoteResponse{QuoteToken: token, RetailQuote: quote}, nil
}

func validateGelvMetadata(o *dbent.PaymentOrder, m map[string]string) error {
	bad := func() error { return fmt.Errorf("gelv payment scope or amount mismatch") }
	if o == nil || o.ProviderSnapshot == nil || len(m) == 0 {
		return bad()
	}
	if m["partner_id"] == "" || m["partner_id"] != psSnapshotStringValue(o.ProviderSnapshot["merchant_id"]) || m["partner_order_id"] != o.OutTradeNo || m["gelv_order_id"] == "" || (o.PaymentTradeNo != "" && m["gelv_order_id"] != o.PaymentTradeNo) || m["currency"] != "CNY" {
		return bad()
	}
	matched := false
	for _, id := range strings.Split(psSnapshotStringValue(o.ProviderSnapshot["gelv_product_ids"]), ",") {
		if strings.TrimSpace(id) == m["product_id"] {
			matched = true
		}
	}
	minor, err := payment.AmountToMinorUnit(payment.FormatAmountForCurrency(o.PayAmount, "CNY"), "CNY")
	if !matched || err != nil || m["amount_minor"] != strconv.FormatInt(minor, 10) || m["product_id"] != psSnapshotStringValue(o.ProviderSnapshot["gelv_product_id"]) {
		return bad()
	}
	return nil
}

func gelvFinalCredit(o *dbent.PaymentOrder, m map[string]string) (float64, error) {
	if err := validateGelvMetadata(o, m); err != nil {
		return 0, err
	}
	if m["gelv_status"] != "paid" || strings.TrimSpace(m["wx_order_id"]) == "" || m["refunded_minor"] != "0" || m["channel_verified"] != "true" || m["channel_verification_status"] != "verified" {
		return 0, fmt.Errorf("gelv payment channel is not verified")
	}
	expectedRate := psSnapshotIntValue(o.ProviderSnapshot["gelv_android_fee_bps"])
	if m["payment_channel"] == "apple_iap" {
		expectedRate = psSnapshotIntValue(o.ProviderSnapshot["gelv_ios_fee_bps"])
	}
	if expectedRate <= 0 || m["fee_rate_bps"] != strconv.Itoa(expectedRate) {
		return 0, fmt.Errorf("gelv payment fee does not match frozen pricing")
	}
	if resolved := psSnapshotStringValue(o.ProviderSnapshot["gelv_payment_channel"]); resolved != "" && resolved != m["payment_channel"] {
		return 0, fmt.Errorf("gelv settled payment channel changed")
	}
	if wxID := psSnapshotStringValue(o.ProviderSnapshot["gelv_wx_order_id"]); wxID != "" && wxID != m["wx_order_id"] {
		return 0, fmt.Errorf("gelv settled platform payment changed")
	}
	q := PaymentOrderRetailQuote(o)
	if q == nil || q.PricingMode != "gelv_channel_cny" || q.CheckoutReference != o.OutTradeNo {
		return 0, fmt.Errorf("gelv payment has no frozen channel pricing")
	}
	credit, ok := q.CreditByChannel[m["payment_channel"]]
	if !ok || !validRetailNumber(credit) {
		return 0, fmt.Errorf("gelv payment channel has no frozen credit amount")
	}
	return credit, nil
}

// Preserve missing settlement values as unknown. These are reconciliation
// evidence, never inputs for selecting the Android/Apple fee or granting credit.
func copyGelvEvidence(snapshot map[string]any, metadata map[string]string) bool {
	changed := false
	for _, key := range []string{"channel_verification_status", "channel_verified_at", "fee_rate_bps", "settlement_state", "platform_fee_fen", "cps_fee_fen", "settled_at"} {
		value, known := metadata[key]
		if !known || value == "" {
			continue
		}
		if key == "settlement_state" && psSnapshotStringValue(snapshot["gelv_settlement_state"]) == "2" && value != "2" {
			continue
		}
		if key == "settled_at" {
			oldTime, _ := strconv.ParseInt(psSnapshotStringValue(snapshot["gelv_settled_at"]), 10, 64)
			newTime, err := strconv.ParseInt(value, 10, 64)
			if err != nil || newTime < oldTime {
				continue
			}
		}
		if psSnapshotStringValue(snapshot["gelv_"+key]) != value {
			snapshot["gelv_"+key] = value
			changed = true
		}
	}
	return changed
}

func (s *PaymentService) syncGelvEvidence(ctx context.Context, o *dbent.PaymentOrder, metadata map[string]string) error {
	if metadata["channel_verification_status"] != "verified" || metadata["channel_verified"] != "true" {
		return nil
	}
	expectedRate := 0
	switch metadata["payment_channel"] {
	case "wechat_virtual":
		expectedRate = psSnapshotIntValue(o.ProviderSnapshot["gelv_android_fee_bps"])
	case "apple_iap":
		expectedRate = psSnapshotIntValue(o.ProviderSnapshot["gelv_ios_fee_bps"])
	}
	wrongChannel := psSnapshotStringValue(o.ProviderSnapshot["gelv_payment_channel"])
	wrongWX := psSnapshotStringValue(o.ProviderSnapshot["gelv_wx_order_id"])
	if expectedRate <= 0 || metadata["fee_rate_bps"] != strconv.Itoa(expectedRate) || (wrongChannel != "" && wrongChannel != metadata["payment_channel"]) || (wrongWX != "" && wrongWX != metadata["wx_order_id"]) {
		s.recordGelvReview(ctx, o, "GELV_CHANNEL_REVIEW_REQUIRED", map[string]any{"reason": "settlement_identity_or_fee_mismatch", "detail": "Keep original payment evidence; reconcile updated channel data manually"})
		return nil
	}
	snapshot := make(map[string]any, len(o.ProviderSnapshot)+7)
	for key, value := range o.ProviderSnapshot {
		snapshot[key] = value
	}
	if !copyGelvEvidence(snapshot, metadata) {
		return nil
	}
	before, err := json.Marshal(o.ProviderSnapshot)
	if err != nil {
		return err
	}
	// Compare the actual JSON evidence and state rather than a timestamp whose
	// representation/precision differs between SQL drivers. A concurrent writer
	// changes the snapshot or state, so its newer evidence cannot be overwritten.
	_, err = s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(o.Status), paymentorder.StatusNEQ(OrderStatusRecharging), func(sel *entsql.Selector) {
		if sel.Dialect() == dialect.SQLite {
			sel.Where(entsql.P(func(b *entsql.Builder) {
				b.WriteString("CAST(").Ident(sel.C(paymentorder.FieldProviderSnapshot)).WriteString(" AS TEXT) = ").Arg(string(before))
			}))
		} else {
			sel.Where(entsql.EQ(sel.C(paymentorder.FieldProviderSnapshot), string(before)))
		}
	}).SetProviderSnapshot(snapshot).Save(ctx)
	return err
}

// ReconcileGelvOrders polls durable local orders, independently of the browser.
// A cancelled/expired local intent can still have an externally paid order.
// Active and historical lanes have separate budgets so refund scans cannot
// starve pending payments. All historical bound orders remain auditable.
func (s *PaymentService) ReconcileGelvOrders(ctx context.Context) (int, error) {
	now := time.Now()
	active := []string{OrderStatusPending, OrderStatusPaid, OrderStatusRecharging, OrderStatusFailed}
	orders, err := s.entClient.PaymentOrder.Query().Where(paymentorder.ProviderKeyEQ(payment.TypeGelv), paymentorder.StatusIn(active...), paymentorder.Or(paymentorder.StatusNEQ(OrderStatusRecharging), paymentorder.UpdatedAtLTE(now.Add(-paymentFulfillmentLeaseDuration)))).Order(dbent.Asc(paymentorder.FieldUpdatedAt)).Limit(15).All(ctx)
	if err != nil {
		return 0, err
	}
	history, err := s.entClient.PaymentOrder.Query().Where(paymentorder.ProviderKeyEQ(payment.TypeGelv), paymentorder.StatusNotIn(active...), paymentorder.PaymentTradeNoNEQ("")).Order(dbent.Asc(paymentorder.FieldUpdatedAt)).Limit(5).All(ctx)
	if err != nil {
		return 0, err
	}
	orders = append(orders, history...)
	recovered := 0
	for _, o := range orders {
		if ctx.Err() != nil {
			return recovered, ctx.Err()
		}
		if s.reconcileGelvOrder(ctx, o) {
			recovered++
		}
	}
	return recovered, nil
}

func (s *PaymentService) reconcileGelvOrder(ctx context.Context, o *dbent.PaymentOrder) bool {
	defer func() {
		// Do not hold a failed order at the front of either scan indefinitely.
		// A live fulfillment lease is never touched by polling bookkeeping.
		touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, _ = s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.StatusNEQ(OrderStatusRecharging)).SetUpdatedAt(time.Now()).Save(touchCtx)
	}()
	prov, err := s.getOrderProvider(ctx, o)
	if err != nil {
		return false
	}
	recovered, err := s.reconcileGelvOrderWithProvider(ctx, o, prov)
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] mini-program order requires reconciliation", "orderID", o.ID, "error", err)
		var upstreamErr *provider.GelvError
		if errors.As(err, &upstreamErr) && (upstreamErr.Kind == "paid_channel_unverified" || upstreamErr.Kind == "paid_channel_conflict" || upstreamErr.Kind == "paid_fee_rule_mismatch") {
			s.recordGelvReview(ctx, o, "GELV_CHANNEL_REVIEW_REQUIRED", map[string]any{"reason": upstreamErr.Kind, "detail": "Payment channel or fee requires review before crediting"})
		}
	}
	return recovered
}

func (s *PaymentService) recordGelvReview(ctx context.Context, o *dbent.PaymentOrder, action string, detail map[string]any) {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return
	}
	exists, err := s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(o.ID, 10)), paymentauditlog.ActionEQ(action), paymentauditlog.DetailEQ(string(encoded))).Exist(ctx)
	if err == nil && !exists {
		s.writeAuditLog(ctx, o.ID, action, payment.TypeGelv, detail)
	}
}

func (s *PaymentService) reconcileGelvOrderWithProvider(ctx context.Context, o *dbent.PaymentOrder, prov payment.Provider) (bool, error) {
	if o.PaymentTradeNo == "" {
		if time.Now().After(o.ExpiresAt) {
			return false, nil
		}
		pr, err := prov.CreatePayment(ctx, payment.CreatePaymentRequest{OrderID: o.OutTradeNo, PaymentType: payment.TypeGelv, Amount: payment.FormatAmountForCurrency(o.PayAmount, "CNY")})
		if err != nil {
			return false, err
		}
		u := s.entClient.PaymentOrder.UpdateOneID(o.ID).SetPaymentTradeNo(pr.TradeNo)
		if pr.QRCode != "" {
			u.SetQrCode(pr.QRCode)
		}
		if _, err = u.Save(ctx); err != nil {
			return false, err
		}
		o.PaymentTradeNo = pr.TradeNo
	}
	resp, err := prov.QueryOrder(ctx, o.PaymentTradeNo)
	if err != nil {
		return false, err
	}
	if resp == nil {
		return false, fmt.Errorf("gelv empty query response")
	}
	if err = validateGelvMetadata(o, resp.Metadata); err != nil {
		return false, err
	}
	if resp.Metadata["channel_verification_status"] == "conflict" {
		s.recordGelvReview(ctx, o, "GELV_CHANNEL_REVIEW_REQUIRED", map[string]any{"reason": "paid_channel_conflict", "detail": "Channel conflict retained; no automatic re-credit"})
		if resp.Status == payment.ProviderStatusPaid {
			return false, fmt.Errorf("gelv payment channel conflict")
		}
	}
	if resp.Status == payment.ProviderStatusPaid {
		if _, err = gelvFinalCredit(o, resp.Metadata); err != nil {
			return false, err
		}
		if o.Status == OrderStatusCompleted {
			return false, s.syncGelvEvidence(ctx, o, resp.Metadata)
		}
		if err = s.confirmPayment(ctx, o.ID, o.PaymentTradeNo, resp.Amount, payment.TypeGelv, resp.Metadata); err != nil {
			return false, err
		}
		return true, nil
	}
	if resp.Status == payment.ProviderStatusRefunded {
		if err = s.syncGelvEvidence(ctx, o, resp.Metadata); err != nil {
			return false, err
		}
		s.recordGelvReview(ctx, o, "GELV_REFUND_REVIEW_REQUIRED", map[string]any{"refunded_minor": resp.Metadata["refunded_minor"], "detail": "Channel refund observed; reconcile site credit and affiliate commission manually"})
	}
	return false, nil
}
