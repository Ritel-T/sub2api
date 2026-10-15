-- Signup credit is distinct from recharge rebates and is granted at most once
-- per invitee. The binding and credit are written in one application transaction.
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_signup_bonus_user
    ON user_affiliate_ledger (user_id)
    WHERE action = 'signup_bonus';
