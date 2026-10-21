package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

// Frozen snapshots and legacy instance bindings remain authoritative even when
// their provider is disabled. The pause must cover those historical orders too.
func PaymentOrderSquarespacePaused(order *dbent.PaymentOrder) bool {
	if !payment.SquarespacePaymentsPaused() || order == nil {
		return false
	}
	isSquarespace := func(key string) bool {
		return strings.EqualFold(strings.TrimSpace(key), payment.TypeSquarespace)
	}
	if isSquarespace(psStringValue(order.ProviderKey)) || isSquarespace(payment.GetBasePaymentType(order.PaymentType)) {
		return true
	}
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil && isSquarespace(snapshot.ProviderKey) {
		return true
	}
	return false
}

func (s *PaymentService) paymentOrderPaused(ctx context.Context, order *dbent.PaymentOrder) bool {
	if !payment.SquarespacePaymentsPaused() || order == nil {
		return false
	}
	if PaymentOrderSquarespacePaused(order) {
		return true
	}
	bound := strings.TrimSpace(psStringValue(order.ProviderInstanceID)) != "" || psOrderProviderSnapshot(order) != nil
	if s == nil || s.entClient == nil {
		return bound || !knownNonSquarespaceOrderIdentity(order)
	}
	inst, err := s.paymentPauseOrderProviderInstance(ctx, order)
	// During the pause an unreadable/missing/mismatched historical binding is
	// unknown. Do not cancel/expire/fulfill it or expose its checkout payload.
	if err != nil {
		// Frozen known non-Square orders retain their existing recovery path
		// when only the historical instance has been deleted. All other read
		// failures and identity conflicts remain unknown and inert.
		if dbent.IsNotFound(err) && confirmedNonSquarespaceOrderIdentity(order) {
			return false
		}
		return true
	}
	if inst == nil {
		return bound || !knownNonSquarespaceOrderIdentity(order)
	}
	return strings.EqualFold(strings.TrimSpace(inst.ProviderKey), payment.TypeSquarespace)
}

func (s *PaymentService) requirePaymentOrderActive(ctx context.Context, order *dbent.PaymentOrder) error {
	if s.paymentOrderPaused(ctx, order) {
		return payment.ErrSquarespacePaymentsPaused
	}
	return nil
}

// User-facing reads retain history while withholding actionable checkout data.
// Copying the entity leaves the stored order and all encrypted evidence intact.
func (s *PaymentService) paymentOrderForResponse(ctx context.Context, order *dbent.PaymentOrder) *dbent.PaymentOrder {
	if !s.paymentOrderPaused(ctx, order) {
		return order
	}
	safe := *order
	safe.PayURL, safe.QrCode, safe.QrCodeImg = nil, nil, nil
	return &safe
}

// Legacy orders with no resolvable instance may still have an explicit known
// provider identity. Unknown identifiers must not be interpreted as non-Square.
func knownNonSquarespaceOrderIdentity(order *dbent.PaymentOrder) bool {
	key := strings.TrimSpace(psStringValue(order.ProviderKey))
	if key == "" {
		key = strings.TrimSpace(order.PaymentType)
	}
	switch payment.GetBasePaymentType(strings.ToLower(key)) {
	case payment.TypeAlipay, payment.TypeWxpay, payment.TypeStripe, payment.TypeEasyPay, payment.TypeAirwallex, payment.TypeGelv, internalBalanceProvider:
		return true
	default:
		return false
	}
}

func confirmedNonSquarespaceOrderIdentity(order *dbent.PaymentOrder) bool {
	key := strings.ToLower(strings.TrimSpace(psStringValue(order.ProviderKey)))
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil {
		snapshotKey := strings.ToLower(strings.TrimSpace(snapshot.ProviderKey))
		if snapshotKey != "" {
			if key != "" && key != snapshotKey {
				return false
			}
			key = snapshotKey
		}
		columnID, snapshotID := strings.TrimSpace(psStringValue(order.ProviderInstanceID)), strings.TrimSpace(snapshot.ProviderInstanceID)
		if columnID != "" && snapshotID != "" && columnID != snapshotID {
			return false
		}
	}
	if key == "" {
		return false
	}
	return knownNonSquarespaceOrderIdentity(&dbent.PaymentOrder{ProviderKey: &key})
}

// Keep typed instance NotFound separate from identity conflicts and DB errors;
// the general resolver intentionally formats some of these for other callers.
func (s *PaymentService) paymentPauseOrderProviderInstance(ctx context.Context, order *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	id := strings.TrimSpace(psStringValue(order.ProviderInstanceID))
	key := strings.TrimSpace(psStringValue(order.ProviderKey))
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil {
		snapshotID, snapshotKey := strings.TrimSpace(snapshot.ProviderInstanceID), strings.TrimSpace(snapshot.ProviderKey)
		if id != "" && snapshotID != "" && id != snapshotID {
			return nil, errors.New("payment pause: conflicting instance identifiers")
		}
		if key != "" && snapshotKey != "" && !strings.EqualFold(key, snapshotKey) {
			return nil, errors.New("payment pause: conflicting provider identities")
		}
		if snapshotID != "" {
			id = snapshotID
		}
		if snapshotKey != "" {
			key = snapshotKey
		}
	}
	if id == "" {
		return s.getOrderProviderInstance(ctx, order)
	}
	instanceID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, err
	}
	instance, err := s.entClient.PaymentProviderInstance.Get(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if key != "" && !strings.EqualFold(key, strings.TrimSpace(instance.ProviderKey)) {
		return nil, errors.New("payment pause: live provider identity changed")
	}
	return instance, nil
}
