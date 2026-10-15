-- Persist every first positive balance recharge, including historical or
-- ineligible ones. Never backfill rewards for a recharge made before rollout.
CREATE INDEX IF NOT EXISTS idx_payment_orders_recharge_code ON payment_orders(recharge_code);

CREATE TABLE IF NOT EXISTS user_affiliate_first_recharges (
    invitee_user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    inviter_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    source_redeem_code_id BIGINT NULL REFERENCES redeem_codes(id) ON DELETE SET NULL,
    source_order_id BIGINT NULL REFERENCES payment_orders(id) ON DELETE SET NULL,
    recharge_amount DECIMAL(20,8) NOT NULL CHECK (recharge_amount > 0),
    reward_amount DECIMAL(20,8) NOT NULL DEFAULT 0 CHECK (reward_amount >= 0),
    outcome VARCHAR(32) NOT NULL CHECK (outcome IN (
        'historical', 'disabled', 'no_inviter', 'below_minimum', 'inviter_unavailable', 'rewarded'
    )),
    first_recharged_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

WITH historical AS (
    SELECT rc.used_by AS user_id, rc.id AS redeem_code_id,
           po.id AS order_id,
           CASE WHEN po.id IS NULL THEN rc.value ELSE po.amount-po.bonus_amount END AS amount,
           COALESCE(rc.used_at, rc.created_at) AS recharged_at
    FROM redeem_codes rc
    LEFT JOIN payment_orders po ON po.recharge_code=rc.code
      AND po.user_id=rc.used_by AND po.order_type='balance' AND po.amount=rc.value
    WHERE rc.type = 'balance' AND rc.value > 0
      AND rc.status = 'used' AND rc.used_by IS NOT NULL
      AND (po.id IS NULL OR po.amount-po.bonus_amount>0)
    UNION ALL
    SELECT po.user_id, NULL::bigint, po.id,
           GREATEST(po.amount - po.bonus_amount, 0),
           COALESCE(po.completed_at, po.paid_at, po.created_at)
    FROM payment_orders po
    JOIN users u ON u.id = po.user_id
    WHERE po.order_type = 'balance'
      AND po.amount - po.bonus_amount > 0
      -- An uncredited provider refund is not a recharge. Refund status alone
      -- does not prove that balance was ever delivered; actual used codes
      -- above and persisted completion do, even after a credited refund.
      AND (po.completed_at IS NOT NULL OR po.status = 'COMPLETED')
), earliest AS (
    SELECT DISTINCT ON (user_id) * FROM historical
    ORDER BY user_id, recharged_at, redeem_code_id NULLS LAST, order_id NULLS LAST
)
INSERT INTO user_affiliate_first_recharges (
    invitee_user_id, source_redeem_code_id, source_order_id,
    recharge_amount, reward_amount, outcome, first_recharged_at
)
SELECT user_id, redeem_code_id, order_id, amount, 0, 'historical', recharged_at
FROM earliest
ON CONFLICT (invitee_user_id) DO NOTHING;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_first_recharge_invitee
    ON user_affiliate_ledger (source_user_id)
    WHERE action = 'first_recharge_bonus';

COMMENT ON TABLE user_affiliate_first_recharges IS
    '首次余额充值及邀请人赠金结果；历史和未达标首充也留存，防止后续补发';
