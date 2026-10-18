-- Billing and the first overflow notice commit atomically. Retain terminal rows:
-- they are also the durable once-per-subscription-window deduplication record.
CREATE TABLE IF NOT EXISTS subscription_overflow_email_outbox (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    subscription_id BIGINT NOT NULL,
    dimension TEXT NOT NULL CHECK (dimension IN ('daily', 'weekly', 'monthly')),
    window_start TIMESTAMPTZ NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'unknown', 'suppressed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claim_token TEXT,
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, subscription_id, dimension, window_start)
);
CREATE INDEX IF NOT EXISTS subscription_overflow_email_pending_idx
    ON subscription_overflow_email_outbox (next_attempt_at, id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS subscription_overflow_email_claimed_idx
    ON subscription_overflow_email_outbox (claimed_at) WHERE status = 'sending';
