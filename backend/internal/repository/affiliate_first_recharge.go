package repository

import (
	"context"
	"fmt"
	"math"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The caller has just credited the recharge in this same transaction, locking
// the invitee's user row. This serializes concurrent payment/code redemptions
// before the first-event check. The PK and unique reward audit are additional
// durable protection against retries and accidental state reset.
func (r *affiliateRepository) RecordFirstRecharge(ctx context.Context, recharge service.AffiliateFirstRecharge) (*service.AffiliateFirstRechargeResult, error) {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return nil, fmt.Errorf("first recharge must share the balance transaction")
	}
	if recharge.InviteeUserID <= 0 || recharge.RedeemCodeID <= 0 || !validFirstRechargeAmount(recharge.Amount, 1e12) || recharge.Amount <= 0 ||
		!validFirstRechargeAmount(recharge.Bonus, service.AffiliateFirstRechargeBonusMax) ||
		!validFirstRechargeAmount(recharge.MinimumAmount, service.AffiliateFirstRechargeMinAmountMax) {
		return nil, fmt.Errorf("invalid first recharge input")
	}
	client := tx.Client()
	// Explicitly retain the row lock even for repository callers outside the
	// usual redemption path; no query relies on total_recharged (admin funding
	// and gifts can be represented there by older releases).
	// NO KEY UPDATE serializes credits while remaining compatible with the
	// KEY SHARE lock taken by another code's used_by foreign-key check. FOR
	// UPDATE here would deadlock when two codes are redeemed concurrently.
	rows, err := client.QueryContext(ctx, "SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR NO KEY UPDATE", recharge.InviteeUserID)
	if err != nil {
		return nil, fmt.Errorf("lock first recharge user: %w", err)
	}
	found := rows.Next()
	readErr := rows.Err()
	_ = rows.Close()
	if readErr != nil {
		return nil, readErr
	}
	if !found {
		return nil, service.ErrUserNotFound
	}
	// Recheck concrete history for callers that predate rollout or for a
	// database restored from a backup. The current used code is excluded.
	rows, err = client.QueryContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM redeem_codes rc
    LEFT JOIN payment_orders po ON po.recharge_code=rc.code
      AND po.user_id=rc.used_by AND po.order_type='balance' AND po.amount=rc.value
    WHERE rc.used_by=$1 AND rc.type='balance' AND rc.status='used' AND rc.value>0 AND rc.id<>$2
      AND (po.id IS NULL OR po.amount-po.bonus_amount>0)
    UNION ALL
    SELECT 1 FROM payment_orders
    WHERE user_id=$1 AND order_type='balance' AND amount-bonus_amount>0
      AND ($3::bigint IS NULL OR id<>$3)
      AND (completed_at IS NOT NULL OR status='COMPLETED')
)`, recharge.InviteeUserID, recharge.RedeemCodeID, nullableInt64Arg(recharge.SourceOrderID))
	if err != nil {
		return nil, fmt.Errorf("read previous recharge history: %w", err)
	}
	var historical bool
	if !rows.Next() {
		_ = rows.Close()
		return nil, fmt.Errorf("previous recharge history returned no row")
	}
	err = rows.Scan(&historical)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}

	summary, err := ensureUserAffiliateWithClient(ctx, client, recharge.InviteeUserID)
	if err != nil {
		return nil, err
	}
	outcome := "rewarded"
	switch {
	case historical:
		outcome = "historical"
	case !recharge.Enabled || recharge.Bonus <= 0:
		outcome = "disabled"
	case summary.InviterID == nil || *summary.InviterID <= 0 || *summary.InviterID == recharge.InviteeUserID:
		outcome = "no_inviter"
	case math.Round(recharge.Amount*1e8) < math.Round(recharge.MinimumAmount*1e8):
		outcome = "below_minimum"
	}

	res, err := client.ExecContext(ctx, `
INSERT INTO user_affiliate_first_recharges (
    invitee_user_id, inviter_user_id, source_redeem_code_id, source_order_id,
    recharge_amount, reward_amount, outcome, first_recharged_at
) VALUES ($1,$2,$3,$4,$5,0,$6,NOW())
ON CONFLICT (invitee_user_id) DO NOTHING`, recharge.InviteeUserID, nullableInt64Arg(summary.InviterID),
		recharge.RedeemCodeID, nullableInt64Arg(recharge.SourceOrderID), recharge.Amount, outcome)
	if err != nil {
		return nil, fmt.Errorf("record first recharge event: %w", err)
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if claimed == 0 || outcome != "rewarded" {
		return nil, nil
	}
	inviterID := *summary.InviterID
	res, err = client.ExecContext(ctx, `UPDATE users SET balance=balance+$1, updated_at=NOW()
WHERE id=$2 AND deleted_at IS NULL`, recharge.Bonus, inviterID)
	if err != nil {
		return nil, fmt.Errorf("credit inviter first recharge bonus: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		_, err = client.ExecContext(ctx, "UPDATE user_affiliate_first_recharges SET outcome='inviter_unavailable' WHERE invitee_user_id=$1", recharge.InviteeUserID)
		return nil, err
	}
	if _, err = ensureUserAffiliateWithClient(ctx, client, inviterID); err != nil {
		return nil, err
	}
	snapshot, err := queryAffiliateTransferSnapshot(ctx, client, inviterID)
	if err != nil {
		return nil, err
	}
	_, err = client.ExecContext(ctx, `
INSERT INTO user_affiliate_ledger (
    user_id, action, amount, source_user_id, source_order_id, balance_after,
    aff_quota_after, aff_frozen_quota_after, aff_history_quota_after, created_at, updated_at
) VALUES ($1,'first_recharge_bonus',$2,$3,$4,$5,$6,$7,$8,NOW(),NOW())`, inviterID, recharge.Bonus,
		recharge.InviteeUserID, nullableInt64Arg(recharge.SourceOrderID), snapshot.BalanceAfter,
		snapshot.AvailableQuotaAfter, snapshot.FrozenQuotaAfter, snapshot.HistoryQuotaAfter)
	if err != nil {
		return nil, fmt.Errorf("record inviter first recharge bonus: %w", err)
	}
	if _, err = client.ExecContext(ctx, "UPDATE user_affiliate_first_recharges SET reward_amount=$1 WHERE invitee_user_id=$2", recharge.Bonus, recharge.InviteeUserID); err != nil {
		return nil, err
	}
	return &service.AffiliateFirstRechargeResult{InviterUserID: inviterID, Amount: recharge.Bonus}, nil
}

func validFirstRechargeAmount(value, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= maximum
}

var _ service.AffiliateFirstRechargeRepository = (*affiliateRepository)(nil)
