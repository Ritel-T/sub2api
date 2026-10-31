package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func (s *PaymentConfigService) validateGelvInstance(ctx context.Context, id int64, key, types string, refund bool) error {
	if key != payment.TypeGelv {
		return nil
	}
	if strings.TrimSpace(types) == "" || refund {
		return infraerrors.BadRequest("INVALID_GELV_CAPABILITY", "Gelv supports balance payments and channel manual refunds only")
	}
	for _, method := range strings.Split(types, ",") {
		if !isGelvPaymentType(strings.TrimSpace(method)) {
			return infraerrors.BadRequest("INVALID_GELV_CAPABILITY", "unsupported Gelv payment method")
		}
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

// quoteGelvPayment keeps mini-program tiers and accepts Alipay cash in cents.
// Site credit includes the separately configured fee, never GBP conversion.
func (s *PaymentService) quoteGelvPayment(ctx context.Context, req CreateOrderRequest, cfg *PaymentConfig) (*RetailQuoteResponse, error) {
	instances, err := s.entClient.PaymentProviderInstance.Query().Where(paymentproviderinstance.ProviderKeyEQ(payment.TypeGelv), paymentproviderinstance.EnabledEQ(true)).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(instances) != 1 || !payment.InstanceSupportsType(instances[0].SupportedTypes, req.PaymentType) {
		return nil, infraerrors.ServiceUnavailable("GELV_NOT_CONFIGURED", "mini-program channel is not configured")
	}
	config, err := s.configService.decryptConfig(instances[0].Config)
	if err != nil {
		return nil, err
	}
	cash := req.Amount
	if req.PaymentType == payment.TypeGelvAlipay {
		if _, err := provider.GelvAlipayNewAmountMinor(strconv.FormatFloat(cash, 'f', -1, 64)); err != nil {
			return nil, infraerrors.BadRequest("INVALID_AMOUNT", "Alipay amount must be 0.01 to 50 CNY with at most two decimal places")
		}
	} else {
		options, err := gelvCreditOptions(req.PaymentType, config)
		if err != nil {
			return nil, infraerrors.ServiceUnavailable("GELV_CREDITS_NOT_CONFIGURED", "mini-program credit amounts are not configured")
		}
		cash = 0
		for _, option := range options {
			if decimal.NewFromFloat(option.Credits).Equal(decimal.NewFromFloat(req.Amount)) {
				cash = option.CashCNY
				break
			}
		}
		if cash == 0 {
			return nil, infraerrors.BadRequest("GELV_DENOMINATION_REQUIRED", "choose one of the configured mini-program recharge tiers")
		}
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
	credits := map[string]float64{"wechat_virtual": android, "apple_iap": ios}
	credited := req.Amount
	if req.PaymentType == payment.TypeGelvAlipay {
		bps, rateErr := provider.GelvAlipayFeeBPS(sel.Config)
		if rateErr != nil {
			return nil, infraerrors.ServiceUnavailable("GELV_CREDITS_NOT_CONFIGURED", "Alipay agreement is not configured")
		}
		credit, creditErr := provider.GelvAlipayCredits(minor, bps)
		if creditErr != nil || !validRetailNumber(credit) {
			return nil, infraerrors.Conflict("PAYMENT_PROVIDER_CHANGED", "Alipay agreement changed; request a new quote")
		}
		credits = map[string]float64{gelvAlipayCheckout: credit}
		credited = credit
	}
	quote := &RetailQuote{PricingMode: "gelv_channel_cny", CreditByChannel: credits, CreditedAmountUSD: credited, PricingBasisCurrency: "CNY", BaseAmountCNY: credited, PayAmount: cash, Currency: "CNY", FX: map[string]float64{}, FXSource: "fixed_cny", FXAsOf: now, IssuedAt: now, ExpiresAt: now.Add(time.Duration(ttl) * time.Second), CheckoutReference: reference}
	if req.PaymentType == payment.TypeGelvAlipay {
		quote.CashAmountInput = true
	} else {
		quote.ProductID, _ = provider.GelvProductForMinor(minor)
	}
	claims := retailQuoteClaims{Purpose: retailQuotePurpose, UserID: req.UserID, PaymentType: req.PaymentType, ProviderInstanceID: sel.InstanceID, ProviderKey: sel.ProviderKey, ProviderIdentity: buildPaymentOrderProviderSnapshot(sel, CreateOrderRequest{PaymentType: req.PaymentType}), Quote: quote}
	token, err := s.signRetailQuote(claims)
	if err != nil {
		return nil, err
	}
	return &RetailQuoteResponse{QuoteToken: token, RetailQuote: quote}, nil
}

const (
	gelvLegacyCheckout = "wechat_virtual"
	gelvAlipayCheckout = "alipay_web"
)

func isGelvPaymentType(method string) bool {
	return method == payment.TypeGelv || method == payment.TypeGelvAlipay
}

func gelvCreditOptions(method string, config map[string]string) ([]provider.GelvCreditOption, error) {
	if method == payment.TypeGelvAlipay {
		return provider.GelvAlipayCreditOptions(config)
	}
	return provider.GelvCreditOptions(config)
}

// Only old legacy orders may omit their checkout channel.
func gelvCheckoutChannel(o *dbent.PaymentOrder) string {
	channel := psSnapshotStringValue(o.ProviderSnapshot["gelv_checkout_channel"])
	if channel == "" && o.PaymentType == payment.TypeGelv {
		return gelvLegacyCheckout
	}
	return channel
}

func gelvRefundHeld(o *dbent.PaymentOrder) bool {
	if o == nil || !isGelvPaymentType(o.PaymentType) {
		return false
	}
	value := o.ProviderSnapshot["gelv_refund_pending"]
	return value == true || value == "true" || psSnapshotStringValue(o.ProviderSnapshot["gelv_reconciliation_blocked"]) != ""
}

func requireGelvNoRefundHold(o *dbent.PaymentOrder) error {
	if gelvRefundHeld(o) {
		return infraerrors.Conflict("GELV_REFUND_PENDING", "channel refund reconciliation is pending")
	}
	return nil
}

func validateGelvMetadata(o *dbent.PaymentOrder, m map[string]string) error {
	bad := func() error { return fmt.Errorf("gelv payment scope or amount mismatch") }
	if o == nil || o.ProviderSnapshot == nil || len(m) == 0 || !isGelvPaymentType(o.PaymentType) || o.OrderType != payment.OrderTypeBalance {
		return bad()
	}
	expectedCheckout := gelvCheckoutChannel(o)
	checkout := m["checkout_channel"]
	if checkout == "" {
		checkout = gelvLegacyCheckout
	}
	if expectedCheckout != checkout || (o.PaymentType == payment.TypeGelvAlipay && checkout != gelvAlipayCheckout) || (o.PaymentType == payment.TypeGelv && checkout != gelvLegacyCheckout) {
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
	q := PaymentOrderRetailQuote(o)
	cashCheckout := o.PaymentType == payment.TypeGelvAlipay && q != nil && q.CashAmountInput
	if cashCheckout {
		matched = m["product_id"] == "" || matched
	}
	minor, err := payment.AmountToMinorUnit(payment.FormatAmountForCurrency(o.PayAmount, "CNY"), "CNY")
	if !matched || err != nil || m["amount_minor"] != strconv.FormatInt(minor, 10) || (!cashCheckout && m["product_id"] != psSnapshotStringValue(o.ProviderSnapshot["gelv_product_id"])) {
		return bad()
	}
	return nil
}

// Validate the partner agreement independently of optional platform fee evidence.
func gelvFrozenCredit(o *dbent.PaymentOrder, m map[string]string) (float64, error) {
	channel := m["payment_channel"]
	if (gelvCheckoutChannel(o) == gelvAlipayCheckout) != (channel == gelvAlipayCheckout) {
		return 0, fmt.Errorf("gelv settled channel differs from checkout")
	}
	identityKey := "wx_order_id"
	if channel == gelvAlipayCheckout {
		identityKey = "alipay_trade_no"
	}
	identity := strings.TrimSpace(m[identityKey])
	if identity == "" {
		return 0, fmt.Errorf("gelv payment identity is missing")
	}
	if resolved := psSnapshotStringValue(o.ProviderSnapshot["gelv_payment_channel"]); resolved != "" && resolved != channel {
		return 0, fmt.Errorf("gelv settled payment channel changed")
	}
	if frozen := psSnapshotStringValue(o.ProviderSnapshot["gelv_"+identityKey]); frozen != "" && frozen != identity {
		return 0, fmt.Errorf("gelv settled platform payment changed")
	}
	minor, err := payment.AmountToMinorUnit(payment.FormatAmountForCurrency(o.PayAmount, "CNY"), "CNY")
	if err != nil {
		return 0, err
	}
	var calculated float64
	if channel == gelvAlipayCheckout {
		bps, rateErr := provider.GelvAlipayFeeBPS(map[string]string{"alipayFeeRateBps": psSnapshotStringValue(o.ProviderSnapshot["gelv_alipay_fee_bps"])})
		if rateErr != nil {
			return 0, fmt.Errorf("gelv missing frozen Alipay agreement")
		}
		calculated, err = provider.GelvAlipayCredits(minor, bps)
	} else {
		expectedRate := psSnapshotIntValue(o.ProviderSnapshot["gelv_android_fee_bps"])
		if channel == "apple_iap" {
			expectedRate = psSnapshotIntValue(o.ProviderSnapshot["gelv_ios_fee_bps"])
		}
		if expectedRate <= 0 || m["fee_rate_bps"] != strconv.Itoa(expectedRate) {
			return 0, fmt.Errorf("gelv payment fee does not match frozen pricing")
		}
		calculated, err = provider.GelvCredits(minor, channel)
	}
	q := PaymentOrderRetailQuote(o)
	if err != nil || q == nil || q.PricingMode != "gelv_channel_cny" || q.CheckoutReference != o.OutTradeNo || q.Currency != "CNY" || q.PayAmount != o.PayAmount {
		return 0, fmt.Errorf("gelv payment has no frozen channel pricing")
	}
	credit, ok := q.CreditByChannel[channel]
	if !ok || !validRetailNumber(credit) || !decimal.NewFromFloat(credit).Equal(decimal.NewFromFloat(calculated)) || (channel == gelvAlipayCheckout && (len(q.CreditByChannel) != 1 || q.CreditedAmountUSD != credit)) {
		return 0, fmt.Errorf("gelv payment channel has invalid frozen credit amount")
	}
	return credit, nil
}

func gelvFinalCredit(o *dbent.PaymentOrder, m map[string]string) (float64, error) {
	if err := validateGelvMetadata(o, m); err != nil {
		return 0, err
	}
	if err := requireGelvNoRefundHold(o); err != nil {
		return 0, err
	}
	if m["gelv_status"] != "paid" || m["refunded_minor"] != "0" || m["refund_pending"] == "true" || m["channel_verified"] != "true" || m["channel_verification_status"] != "verified" {
		return 0, fmt.Errorf("gelv payment channel is not verified or refund is pending")
	}
	return gelvFrozenCredit(o, m)
}

// Preserve missing settlement values as unknown. These are reconciliation
// evidence, never inputs for selecting the Android/Apple fee or granting credit.
func copyGelvEvidence(snapshot map[string]any, metadata map[string]string) bool {
	changed := false
	for _, key := range []string{"channel_verification_status", "channel_verified_at", "refunded_minor", "fee_rate_bps", "settlement_state", "platform_fee_fen", "cps_fee_fen", "settled_at"} {
		value, known := metadata[key]
		if !known || value == "" {
			continue
		}
		if key == "refunded_minor" {
			previous, _ := strconv.ParseInt(psSnapshotStringValue(snapshot["gelv_refunded_minor"]), 10, 64)
			incoming, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil || incoming < previous {
				continue
			}
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
	if _, err := gelvFrozenCredit(o, metadata); err != nil {
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

const (
	// Six calls per 5s (72/min steady state) leaves headroom under Gelv's
	// shared 120/min peer quota for history, checkout and user-driven recovery.
	// Larger backlogs rotate fairly but cannot promise ~10s settlement.
	gelvActiveScanLimit   = 6
	gelvActiveConcurrency = 3
	gelvOrderPollLockTTL  = time.Minute
)

type gelvReconcileLocksKey struct{}
type gelvReconcileLocks struct {
	cache LeaderLockCache
	db    *sql.DB
}
type gelvActiveReconcileKey struct{}

// This also guards callers without a coordination backend (unit/single-instance
// mode). Entries exist only while a poll is in flight, never for order history.
var gelvOrderPolls sync.Map

type gelvOrderPollKey struct {
	client *dbent.Client
	id     int64
}

func gelvActiveStatuses() []string {
	return []string{OrderStatusPending, OrderStatusPaid, OrderStatusRecharging, OrderStatusFailed}
}

// Match the durable hold representation in gelvRefundHeld, with explicit
// COALESCE so NOT(eligible) also partitions snapshots with missing JSON keys.
func gelvFastNotHeld(sel *entsql.Selector) {
	sel.Where(entsql.P(func(b *entsql.Builder) {
		col := sel.C(paymentorder.FieldProviderSnapshot)
		if sel.Dialect() == dialect.SQLite {
			b.WriteString("COALESCE(json_extract(").Ident(col).WriteString(", '$.gelv_reconciliation_blocked'), '') = '' AND COALESCE(CAST(json_extract(").Ident(col).WriteString(", '$.gelv_refund_pending') AS TEXT), '') NOT IN ('true', '1')")
		} else {
			b.WriteString("COALESCE(").Ident(col).WriteString("->>'gelv_reconciliation_blocked', '') = '' AND COALESCE(").Ident(col).WriteString("->>'gelv_refund_pending', '') NOT IN ('true', '1')")
		}
	}))
}

func gelvLanePredicate(active bool) predicate.PaymentOrder {
	eligible := paymentorder.And(paymentorder.StatusIn(gelvActiveStatuses()...), gelvFastNotHeld)
	lease := paymentorder.Or(paymentorder.StatusNEQ(OrderStatusRecharging), paymentorder.UpdatedAtLTE(time.Now().Add(-paymentFulfillmentLeaseDuration)))
	if !active {
		// This is the fast eligibility complement, not merely non-active statuses:
		// held PAID/FAILED records must still query signed authority at 60s so refund
		// holds can clear. Bound cancelled/expired intents retain late recovery.
		// Neither lane ever polls a live fulfillment lease.
		return paymentorder.And(paymentorder.Not(eligible), paymentorder.PaymentTradeNoNEQ(""), lease)
	}
	return paymentorder.And(eligible, lease)
}

func (s *PaymentService) gelvOrdersForLane(ctx context.Context, active bool, limit int) ([]*dbent.PaymentOrder, error) {
	return s.entClient.PaymentOrder.Query().Where(paymentorder.ProviderKeyEQ(payment.TypeGelv), gelvLanePredicate(active)).
		Order(dbent.Asc(paymentorder.FieldUpdatedAt), dbent.Asc(paymentorder.FieldID)).Limit(limit).All(ctx)
}

// ReconcileGelvOrders is the historical lane only. The general 60s expiry job
// must never launch a second ACTIVE sweep alongside the independent fast lane.
func (s *PaymentService) ReconcileGelvOrders(ctx context.Context) (int, error) {
	orders, err := s.gelvOrdersForLane(ctx, false, 5)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, o := range orders {
		if ctx.Err() != nil {
			return recovered, ctx.Err()
		}
		if s.reconcileGelvLaneOrder(ctx, o.ID, false) {
			recovered++
		}
	}
	return recovered, ctx.Err()
}

// ReconcileGelvActiveOrders takes a fresh durable selection each cycle. Oldest
// attempts rotate via updated_at, including errors, so an unavailable first
// order cannot monopolize the scan. Workers share a bounded query deadline.
func (s *PaymentService) ReconcileGelvActiveOrders(ctx context.Context) (int, error) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, gelvActiveCheckTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, gelvActiveReconcileKey{}, true)
	orders, err := s.gelvOrdersForLane(ctx, true, gelvActiveScanLimit)
	if err != nil {
		return 0, err
	}
	var recovered atomic.Int64
	jobs := make(chan int64)
	var wg sync.WaitGroup
	for range gelvActiveConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if ctx.Err() != nil {
					return
				}
				if s.reconcileGelvLaneOrder(ctx, id, true) {
					recovered.Add(1)
				}
			}
		}()
	}
dispatch:
	for _, o := range orders {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- o.ID:
		}
	}
	close(jobs)
	// Drain before releasing the cycle leader or returning from Stop. In
	// particular, fulfillment must not outlive the leader's safe TTL window.
	wg.Wait()
	if len(orders) > 0 {
		slog.Info("[PaymentOrderExpiry] active mini-program reconciliation", "selected", len(orders), "recovered", recovered.Load(), "duration_ms", time.Since(started).Milliseconds())
	}
	return int(recovered.Load()), ctx.Err()
}

func (s *PaymentService) reconcileGelvLaneOrder(ctx context.Context, id int64, active bool) bool {
	key := gelvOrderPollKey{s.entClient, id}
	if _, busy := gelvOrderPolls.LoadOrStore(key, struct{}{}); busy {
		return false
	}
	defer gelvOrderPolls.Delete(key)
	if locks, ok := ctx.Value(gelvReconcileLocksKey{}).(gelvReconcileLocks); ok {
		release, acquired := tryAcquireSingletonLeaderLock(ctx, locks.cache, locks.db, fmt.Sprintf("payment:gelv:order:%d:poll", id), uuid.NewString(), gelvOrderPollLockTTL)
		if !acquired {
			return false
		}
		defer release()
	}
	// Re-read after acquiring the order guard: a queued candidate may have been
	// fulfilled, cancelled, held, or acquired a live fulfillment lease meanwhile.
	o, err := s.entClient.PaymentOrder.Query().Where(paymentorder.IDEQ(id), paymentorder.ProviderKeyEQ(payment.TypeGelv), gelvLanePredicate(active)).Only(ctx)
	if err != nil {
		return false
	}
	return s.reconcileGelvOrder(ctx, o)
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
		pr, err := prov.CreatePayment(provider.WithGelvPaymentRecovery(ctx, o.OutTradeNo), payment.CreatePaymentRequest{OrderID: o.OutTradeNo, PaymentType: o.PaymentType, Amount: payment.FormatAmountForCurrency(o.PayAmount, "CNY")})
		if err != nil {
			return false, err
		}
		if pr != nil && (o.PaymentType == payment.TypeGelvAlipay || len(pr.Metadata) > 0) {
			if err = validateGelvMetadata(o, pr.Metadata); err != nil {
				return false, err
			}
		}
		if pr == nil || pr.TradeNo == "" {
			return false, fmt.Errorf("gelv missing created order identity")
		}
		if len(pr.Metadata) > 0 && pr.TradeNo != pr.Metadata["gelv_order_id"] {
			return false, fmt.Errorf("gelv created identity mismatch")
		}
		u := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), paymentorder.PaymentTradeNoEQ(""), gelvSnapshotUnchanged(o)).SetPaymentTradeNo(pr.TradeNo)
		if pr.PayURL != "" {
			u.SetPayURL(pr.PayURL)
		}
		if pr.QRCode != "" {
			u.SetQrCode(pr.QRCode)
		}
		count, saveErr := u.Save(ctx)
		if saveErr != nil {
			return false, saveErr
		}
		if count == 0 {
			current, loadErr := s.entClient.PaymentOrder.Get(ctx, o.ID)
			if loadErr != nil {
				return false, loadErr
			}
			if current.PaymentTradeNo != pr.TradeNo {
				return false, fmt.Errorf("gelv created order binding changed")
			}
			*o = *current
		} else {
			o.PaymentTradeNo = pr.TradeNo
		}
		if fast, _ := ctx.Value(gelvActiveReconcileKey{}).(bool); fast {
			// Creation recovery replays this same source identity only. Spend at most
			// one upstream call per fast slot; query the restored binding next cycle.
			return false, nil
		}
	}
	observation := gelvQueryObservation(o)
	resp, err := prov.QueryOrder(ctx, o.PaymentTradeNo)
	if err != nil {
		if holdErr := s.holdGelvQueryError(ctx, o, err); holdErr != nil {
			return false, holdErr
		}
		return false, err
	}
	if resp == nil {
		if o.PaidAt != nil {
			if blockErr := s.persistGelvReconciliationBlock(ctx, o, "empty_query_response", nil); blockErr != nil {
				return false, blockErr
			}
		}
		return false, fmt.Errorf("gelv empty query response")
	}
	if err = validateGelvMetadata(o, resp.Metadata); err != nil {
		if o.PaidAt != nil {
			if blockErr := s.persistGelvReconciliationBlock(ctx, o, "query_scope_or_amount_mismatch", nil); blockErr != nil {
				return false, blockErr
			}
		}
		return false, err
	}
	if resp.Metadata["refund_pending"] == "true" {
		return false, s.holdGelvRefund(ctx, o, resp.Metadata)
	}
	if err = s.clearGelvRefundHold(ctx, observation, resp.Metadata); err != nil {
		return false, err
	}
	o.ProviderSnapshot = observation.ProviderSnapshot
	if refunded, parseErr := strconv.ParseInt(resp.Metadata["refunded_minor"], 10, 64); parseErr == nil && refunded > 0 {
		if err = s.persistGelvReconciliationBlock(ctx, o, "confirmed_refund", resp.Metadata); err != nil {
			return false, err
		}
	}
	if gelvRefundHeld(o) && resp.Status == payment.ProviderStatusPaid {
		return false, requireGelvNoRefundHold(o)
	}
	if resp.Metadata["channel_verification_status"] == "conflict" {
		if err = s.persistGelvReconciliationBlock(ctx, o, "paid_channel_conflict", resp.Metadata); err != nil {
			return false, err
		}
		s.recordGelvReview(ctx, o, "GELV_CHANNEL_REVIEW_REQUIRED", map[string]any{"reason": "paid_channel_conflict", "detail": "Channel conflict retained; no automatic re-credit"})
		if resp.Status == payment.ProviderStatusPaid {
			return false, fmt.Errorf("gelv payment channel conflict")
		}
	}
	if resp.Status == payment.ProviderStatusPaid {
		if _, err = gelvFinalCredit(o, resp.Metadata); err != nil {
			if blockErr := s.persistGelvReconciliationBlock(ctx, o, "paid_evidence_mismatch", resp.Metadata); blockErr != nil {
				return false, blockErr
			}
			return false, err
		}
		if o.Status == OrderStatusCompleted {
			return false, s.syncGelvEvidence(ctx, o, resp.Metadata)
		}
		fulfillCtx := ctx
		if fast, _ := ctx.Value(gelvActiveReconcileKey{}).(bool); fast {
			// The short poll budget bounds upstream I/O, not committed redemption.
			// Preserve values and every original validation/lease/CAS guard, while
			// allowing the original fulfillment path to drain after the query deadline.
			var cancel context.CancelFunc
			fulfillCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), gelvActiveFulfillmentTimeout)
			defer cancel()
		}
		if err = s.confirmPayment(fulfillCtx, o.ID, o.PaymentTradeNo, resp.Amount, payment.TypeGelv, resp.Metadata); err != nil {
			return false, err
		}
		return true, nil
	}
	if resp.Status == payment.ProviderStatusRefunded {
		if err = s.syncGelvEvidence(ctx, o, resp.Metadata); err != nil {
			return false, err
		}
		s.recordGelvReview(ctx, o, "GELV_REFUND_REVIEW_REQUIRED", map[string]any{"refunded_minor": resp.Metadata["refunded_minor"], "alipay_trade_no": resp.Metadata["alipay_trade_no"], "wx_order_id": resp.Metadata["wx_order_id"], "checkout_channel": resp.Metadata["checkout_channel"], "detail": "Channel refund observed; reconcile site credit and affiliate commission manually"})
	}
	return false, nil
}

// A hold can only be cleared by an explicit signed query result. Lock the row also used
// by redemption so a refund observation cannot race a subsequent balance credit.
func (s *PaymentService) holdGelvRefund(ctx context.Context, o *dbent.PaymentOrder, metadata map[string]string) error {
	if err := validateGelvMetadata(o, metadata); err != nil {
		return err
	}
	return s.persistGelvReconciliationBlock(ctx, o, "refund_pending", metadata)
}

// The row lock is shared with redemption. Repeated pending holds always advance
// generation to invalidate a delayed clear query. Permanent blocks advance only
// when their retained evidence changes; identical adverse queries do not write.
func (s *PaymentService) persistGelvReconciliationBlock(ctx context.Context, o *dbent.PaymentOrder, reason string, metadata map[string]string) error {
	if len(metadata) > 0 {
		if err := validateGelvMetadata(o, metadata); err != nil {
			return err
		}
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := tx.PaymentOrder.Query().Where(paymentorder.IDEQ(o.ID))
	if s.entClient.Driver().Dialect() == dialect.Postgres {
		query = query.ForUpdate()
	}
	current, err := query.Only(ctx)
	if err != nil {
		return err
	}
	if len(metadata) > 0 {
		if err = validateGelvMetadata(current, metadata); err != nil {
			return err
		}
	}
	snapshot := make(map[string]any, len(current.ProviderSnapshot)+6)
	for key, value := range current.ProviderSnapshot {
		snapshot[key] = value
	}
	changed := reason == "refund_pending" || metadata["refund_pending"] == "true"
	if reason == "refund_pending" {
		snapshot["gelv_refund_pending"] = "true"
	} else if snapshot["gelv_reconciliation_blocked"] != "confirmed_refund" && snapshot["gelv_reconciliation_blocked"] != reason {
		snapshot["gelv_reconciliation_blocked"] = reason
		changed = true
	}
	// Confirmed refunded cash is monotonic. A stale zero or smaller partial
	// observation cannot erase an already observed refund or its blocker.
	previous, previousErr := strconv.ParseInt(psSnapshotStringValue(snapshot["gelv_refunded_minor"]), 10, 64)
	if value, known := metadata["refunded_minor"]; known && value != "" {
		incoming, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr == nil && incoming >= 0 && (previousErr != nil || incoming > previous) {
			previous = incoming
			snapshot["gelv_refunded_minor"] = strconv.FormatInt(incoming, 10)
			changed = true
		}
	}
	if previous > 0 && snapshot["gelv_reconciliation_blocked"] != "confirmed_refund" {
		snapshot["gelv_reconciliation_blocked"] = "confirmed_refund"
		changed = true
	}
	if value := metadata["channel_verification_status"]; value != "" && snapshot["gelv_observed_verification_status"] != value {
		snapshot["gelv_observed_verification_status"] = value
		changed = true
	}
	if !changed {
		// Refresh a stale caller from the locked row without changing its
		// snapshot, updated_at, generation, or audit trail.
		o.ProviderSnapshot = snapshot
		return nil
	}
	generation := psSnapshotIntValue(snapshot["gelv_observation_generation"])
	snapshot["gelv_observation_generation"] = strconv.Itoa(generation + 1)
	if _, err = tx.PaymentOrder.UpdateOneID(o.ID).SetProviderSnapshot(snapshot).Save(ctx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	o.ProviderSnapshot = snapshot
	action := "GELV_RECONCILIATION_HOLD"
	if reason == "refund_pending" {
		action = "GELV_REFUND_PENDING_HOLD"
	}
	s.recordGelvReview(ctx, o, action, map[string]any{"reason": reason, "refunded_minor": metadata["refunded_minor"], "refund_pending": metadata["refund_pending"], "checkout_channel": metadata["checkout_channel"], "alipay_trade_no": metadata["alipay_trade_no"], "wx_order_id": metadata["wx_order_id"], "detail": "Channel reconciliation required; automatic fulfillment and recharge retry held"})
	return nil
}

func gelvQueryObservation(o *dbent.PaymentOrder) *dbent.PaymentOrder {
	observed := *o
	observed.ProviderSnapshot = make(map[string]any, len(o.ProviderSnapshot))
	for key, value := range o.ProviderSnapshot {
		observed.ProviderSnapshot[key] = value
	}
	return &observed
}

// Reuse the production JSON unique index for both receipt namespaces. This
// snapshot field is a compatibility uniqueness key, not Alipay WeChat evidence.
func gelvPlatformIdentity(metadata map[string]string) string {
	if metadata["payment_channel"] == gelvAlipayCheckout {
		return "alipay:" + metadata["alipay_trade_no"]
	}
	return metadata["wx_order_id"]
}

func gelvSnapshotUnchanged(o *dbent.PaymentOrder) func(*entsql.Selector) {
	before, err := json.Marshal(o.ProviderSnapshot)
	return func(sel *entsql.Selector) {
		if err != nil {
			sel.Where(entsql.False())
			return
		}
		if sel.Dialect() == dialect.SQLite {
			sel.Where(entsql.P(func(b *entsql.Builder) {
				b.WriteString("CAST(").Ident(sel.C(paymentorder.FieldProviderSnapshot)).WriteString(" AS TEXT) = ").Arg(string(before))
			}))
		} else {
			sel.Where(entsql.EQ(sel.C(paymentorder.FieldProviderSnapshot), string(before)))
		}
	}
}

// PaymentOrderRefundReconciliationPending is a public review signal, independent
// of order/refund status and of the confirmed refunded amount (which may be zero).
func PaymentOrderRefundReconciliationPending(o *dbent.PaymentOrder) bool { return gelvRefundHeld(o) }

// Only invoked after a successful scoped signed QueryOrder response. Missing
// flags and confirmed refunds cannot clear a hold or authorize a recharge retry.
func (s *PaymentService) clearGelvRefundHold(ctx context.Context, o *dbent.PaymentOrder, metadata map[string]string) error {
	if (o.ProviderSnapshot["gelv_refund_pending"] != "true" && o.ProviderSnapshot["gelv_refund_pending"] != true) || metadata["refund_pending"] != "false" || metadata["refunded_minor"] != "0" {
		return nil
	}
	if o.ProviderSnapshot["gelv_reconciliation_blocked"] == "confirmed_refund" {
		return nil
	}
	if raw, known := o.ProviderSnapshot["gelv_refunded_minor"]; known {
		confirmed, parseErr := strconv.ParseInt(psSnapshotStringValue(raw), 10, 64)
		if parseErr != nil || confirmed != 0 {
			return nil
		}
	}
	if err := validateGelvMetadata(o, metadata); err != nil {
		return err
	}
	snapshot := make(map[string]any, len(o.ProviderSnapshot))
	for key, value := range o.ProviderSnapshot {
		snapshot[key] = value
	}
	snapshot["gelv_refund_pending"] = "false"
	snapshot["gelv_refunded_minor"] = "0"
	count, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(o.ID), gelvSnapshotUnchanged(o)).SetProviderSnapshot(snapshot).Save(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		return infraerrors.Conflict("GELV_REFUND_HOLD_CHANGED", "refund observation changed; query again")
	}
	o.ProviderSnapshot = snapshot
	s.recordGelvReview(ctx, o, "GELV_REFUND_HOLD_CLEARED", map[string]any{"refund_pending": false, "refunded_minor": "0", "detail": "Authoritative query cleared pending refund"})
	return nil
}

// Recover only the persisted provider, method and agreement even if current
// quoting configuration changes after an uncertain create response.
func (s *PaymentService) gelvReplaySelection(ctx context.Context, o *dbent.PaymentOrder) (*payment.InstanceSelection, error) {
	inst, err := s.getOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.ProviderKey != payment.TypeGelv {
		return nil, fmt.Errorf("gelv historical instance unavailable")
	}
	config, err := s.configService.decryptConfig(inst.Config)
	if err != nil {
		return nil, err
	}
	if config["partnerId"] != psSnapshotStringValue(o.ProviderSnapshot["merchant_id"]) {
		return nil, fmt.Errorf("gelv historical partner changed")
	}
	if o.PaymentType == payment.TypeGelvAlipay {
		bps, rateErr := provider.GelvAlipayFeeBPS(map[string]string{"alipayFeeRateBps": psSnapshotStringValue(o.ProviderSnapshot["gelv_alipay_fee_bps"])})
		if rateErr != nil {
			return nil, rateErr
		}
		config["alipayFeeRateBps"] = strconv.FormatInt(bps, 10)
	}
	return &payment.InstanceSelection{InstanceID: strconv.FormatInt(inst.ID, 10), ProviderKey: payment.TypeGelv, Config: config, SupportedTypes: inst.SupportedTypes, PaymentMode: inst.PaymentMode}, nil
}

// Retries use the finalized durable evidence, not a client amount or a new fee
// schedule. This also protects direct/admin retries after a partial fulfillment.
func requireGelvFulfillmentEvidence(o *dbent.PaymentOrder) error {
	if o == nil || !isGelvPaymentType(o.PaymentType) {
		return nil
	}
	if err := requireGelvNoRefundHold(o); err != nil {
		return err
	}
	if o.OrderType != payment.OrderTypeBalance || o.ProviderSnapshot["gelv_channel_verification_status"] != "verified" {
		return fmt.Errorf("gelv fulfillment has no verified payment evidence")
	}
	m := map[string]string{}
	for _, key := range []string{"payment_channel", "alipay_trade_no", "wx_order_id", "fee_rate_bps"} {
		m[key] = psSnapshotStringValue(o.ProviderSnapshot["gelv_"+key])
	}
	if m["payment_channel"] == gelvAlipayCheckout && m["wx_order_id"] != "alipay:"+m["alipay_trade_no"] {
		return fmt.Errorf("gelv Alipay indexed identity mismatch")
	}
	if refunded, known := o.ProviderSnapshot["gelv_refunded_minor"]; known && refunded != "0" {
		return fmt.Errorf("gelv refund evidence requires reconciliation")
	}
	credit, err := gelvFrozenCredit(o, m)
	if err != nil {
		return err
	}
	final, err := decimal.NewFromString(fmt.Sprint(o.ProviderSnapshot["gelv_final_credit"]))
	if err != nil || !final.Equal(decimal.NewFromFloat(credit)) || !final.Equal(decimal.NewFromFloat(o.Amount)) {
		return fmt.Errorf("gelv persisted credit differs from verified frozen pricing")
	}
	return nil
}

func (s *PaymentService) holdGelvQueryError(ctx context.Context, o *dbent.PaymentOrder, queryErr error) error {
	var upstream *provider.GelvError
	if !errors.As(queryErr, &upstream) {
		return nil
	}
	adverse := false
	switch upstream.Kind {
	case "paid_channel_unverified", "paid_channel_conflict", "paid_fee_rule_mismatch":
		adverse = true
	case "incomplete_paid_evidence", "invalid_order_scope_or_amount", "product_denomination_mismatch", "unknown_order_status", "checkout_channel_mismatch", "invalid_checkout_channel", "inconsistent_refund_state", "queried_order_mismatch", "invalid_response", "invalid_response_size":
		adverse = o.PaidAt != nil
	}
	if adverse {
		return s.persistGelvReconciliationBlock(ctx, o, upstream.Kind, nil)
	}
	// Transport failures and HTTP availability errors do not imply a refund or
	// invalidate previously verified paid evidence.
	return nil
}
