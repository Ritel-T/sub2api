package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentexternalpayment"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
)

func (s *SquarespacePaymentBridge) refundCreditProof(ctx context.Context, local *dbent.PaymentOrder) (bool, error) {
	if local == nil || s.payment.redeemService == nil {
		return false, errors.New("payment redeem proof unavailable")
	}
	code, err := s.payment.redeemService.GetByCode(ctx, local.RechargeCode)
	if errors.Is(err, ErrRedeemCodeNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if code == nil {
		return false, nil
	}
	if err = validatePaymentRedeemCode(local, code); err != nil {
		return false, err
	}
	return code.IsUsed(), nil
}

// applyObservedRefund records a refund already proved by Squarespace GETs. It
// never calls the provider Refund API or creates another balance recharge.
func (s *SquarespacePaymentBridge) applyObservedRefund(ctx context.Context, observed *dbent.PaymentExternalOrder, proof *verifiedSquarespaceProof, paymentState string) (err error) {
	if observed.LocalOrderID == nil {
		return errors.New("unbound refund observation")
	}
	local, err := s.client.PaymentOrder.Get(ctx, *observed.LocalOrderID)
	if err != nil {
		return err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	ledgerQuery := tx.PaymentExternalOrder.Query().Where(paymentexternalorder.IDEQ(observed.ID))
	localQuery := tx.PaymentOrder.Query().Where(paymentorder.IDEQ(local.ID))
	if s.client.Driver().Dialect() == dialect.Postgres {
		ledgerQuery.ForUpdate()
		localQuery.ForUpdate()
	}
	local, err = localQuery.Only(ctx)
	if err != nil {
		return err
	}
	ledger, err := ledgerQuery.Only(ctx)
	if err != nil {
		return err
	}
	credited, err := s.refundCreditProof(dbent.NewTxContext(ctx, tx), local)
	if err != nil {
		return err
	}
	if ledger.LocalOrderID == nil || *ledger.LocalOrderID != local.ID || ledger.TotalMinor != proof.TotalMinor || ledger.Currency != proof.Currency {
		return errors.New("refund binding changed")
	}
	if proof.RefundedMinor < ledger.RefundedMinor {
		_ = tx.Rollback()
		return s.anomaly(ctx, ledger.ID, "REFUND_TOTAL_REGRESSED")
	}
	target := int64(0)
	if credited {
		target, err = squarespaceRefundTarget(ledger.CreditedUsdUnits, ledger.TotalMinor, proof.RefundedMinor)
		if err != nil {
			return err
		}
	}
	if target < ledger.RefundTargetUsdUnits {
		return errors.New("refund entitlement target regressed")
	}
	deltaTarget := target - ledger.RefundTargetUsdUnits
	deltaRefund := proof.RefundedMinor - ledger.RefundedMinor
	recovered := int64(0)
	if deltaTarget > 0 {
		deducted, deductErr := s.payment.deductAvailableBalance(dbent.NewTxContext(ctx, tx), local.UserID, float64(deltaTarget)/float64(squarespaceUSDScale))
		if deductErr != nil {
			return deductErr
		}
		if math.IsNaN(deducted) || math.IsInf(deducted, 0) || deducted < 0 {
			return errors.New("invalid actual refund deduction")
		}
		recovered = int64(math.Round(deducted * float64(squarespaceUSDScale)))
		if recovered < 0 || recovered > deltaTarget {
			return errors.New("refund deduction exceeds verified target")
		}
	}
	debt := deltaTarget - recovered
	journalStatus := "RECOVERED"
	if !credited {
		journalStatus = "UNCREDITED"
	}
	if debt > 0 {
		journalStatus = "DEBT"
	}
	if deltaRefund > 0 {
		_, err = tx.PaymentExternalRefundJournal.Create().SetExternalOrderLedgerID(ledger.ID).SetCumulativeRefundMinor(proof.RefundedMinor).SetDeltaRefundMinor(deltaRefund).SetTargetUsdUnits(target).SetDeltaTargetUsdUnits(deltaTarget).SetRecoveredUsdUnits(recovered).SetDebtUsdUnits(debt).SetStatus(journalStatus).Save(ctx)
		if err != nil {
			return err
		}
	}
	status := "REFUND_WAITING"
	localStatus := OrderStatusRefundPending
	if proof.RefundedMinor > 0 && credited {
		status = "PARTIALLY_REFUNDED"
		localStatus = OrderStatusPartiallyRefunded
	}
	if proof.RefundedMinor == proof.TotalMinor {
		status = "REFUNDED"
		localStatus = OrderStatusRefunded
	}
	code := ""
	if debt > 0 || ledger.DebtUsdUnits > 0 {
		code = "REFUND_BALANCE_SHORTFALL"
	}
	// Cash recovery and durable totals form one transaction. A completed payment
	// never becomes eligible for fulfillment again after any refund observation.
	_, err = tx.PaymentExternalOrder.UpdateOneID(ledger.ID).SetRefundedMinor(proof.RefundedMinor).SetRefundTargetUsdUnits(target).AddRecoveredUsdUnits(recovered).AddDebtUsdUnits(debt).SetStatus(status).SetAnomalyCode(code).Save(ctx)
	if err != nil {
		return err
	}
	for _, paid := range proof.Payments {
		count, updateErr := tx.PaymentExternalPayment.Update().Where(paymentexternalpayment.ExternalOrderLedgerIDEQ(ledger.ID), paymentexternalpayment.PaymentIDEQ(paid.ID)).SetRefundedMinor(paid.RefundedMinor).Save(ctx)
		if updateErr != nil {
			return updateErr
		}
		if count != 1 {
			return errors.New("refund payment ledger changed")
		}
	}

	if !credited || proof.RefundedMinor > 0 {
		_, err = tx.PaymentOrder.UpdateOneID(local.ID).SetStatus(localStatus).SetRefundAmount(float64(proof.RefundedMinor) / 100).SetRefundAt(s.now()).Save(ctx)
		if err != nil {
			return err
		}
	}
	if deltaRefund > 0 {
		details, _ := json.Marshal(map[string]any{"external_ledger_id": ledger.ID, "cumulative_refund_minor": proof.RefundedMinor, "target_usd_units": target, "recovered_usd_units": recovered, "debt_usd_units": debt})
		_, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(local.ID, 10)).SetAction("SQUARESPACE_EXTERNAL_REFUND").SetOperator("system").SetDetail(string(details)).Save(ctx)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	refreshed, getErr := s.client.PaymentExternalOrder.Get(ctx, ledger.ID)
	if getErr != nil {
		return getErr
	}
	return s.invalidateRefundCaches(ctx, refreshed)
}

func (s *SquarespacePaymentBridge) invalidateRefundCaches(ctx context.Context, ledger *dbent.PaymentExternalOrder) error {
	if ledger == nil || ledger.LocalOrderID == nil {
		return nil
	}
	local, err := s.client.PaymentOrder.Get(ctx, *ledger.LocalOrderID)
	if err != nil {
		return err
	}
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if s.balanceCache == nil {
		return s.anomaly(ctx, ledger.ID, "CACHE_INVALIDATION_PENDING")
	}
	if err = s.balanceCache.InvalidateUserBalance(cacheCtx, local.UserID); err != nil {
		_ = s.anomaly(ctx, ledger.ID, "CACHE_INVALIDATION_PENDING")
		return errors.New("refund balance cache invalidation pending")
	}
	if s.authCache != nil {
		s.authCache.InvalidateAuthCacheByUserID(cacheCtx, local.UserID)
	}
	if ledger.AnomalyCode == "CACHE_INVALIDATION_PENDING" {
		code := ""
		if ledger.DebtUsdUnits > 0 {
			code = "REFUND_BALANCE_SHORTFALL"
		}
		return s.client.PaymentExternalOrder.UpdateOneID(ledger.ID).SetAnomalyCode(code).Exec(ctx)
	}
	return nil
}
