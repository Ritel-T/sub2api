-- 233_managed_scheduled_test_recovery.sql
-- Managed OpenAI quota-recovery test plans and narrow recovery audit state.

CREATE TABLE IF NOT EXISTS managed_scheduled_test_templates (
    template_key        VARCHAR(100) PRIMARY KEY,
    platform            VARCHAR(50) NOT NULL,
    account_type        VARCHAR(50) NOT NULL,
    model_id            VARCHAR(100) NOT NULL,
    interval_minutes    INT NOT NULL,
    shard_start_minute  INT NOT NULL,
    shard_count         INT NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT false,
    auto_recover        BOOLEAN NOT NULL DEFAULT true,
    only_when_blocked   BOOLEAN NOT NULL DEFAULT true,
    recover_scope       VARCHAR(50) NOT NULL DEFAULT 'account_rate_limit_only',
    max_results         INT NOT NULL DEFAULT 20,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT managed_scheduled_test_templates_recover_scope_check
        CHECK (recover_scope IN ('full', 'account_rate_limit_only')),
    CONSTRAINT managed_scheduled_test_templates_interval_check
        CHECK (interval_minutes > 0 AND interval_minutes < 60),
    CONSTRAINT managed_scheduled_test_templates_shard_check
        CHECK (
            shard_start_minute >= 0
            AND shard_start_minute < 60
            AND shard_count > 0
            AND shard_start_minute + shard_count <= interval_minutes
        ),
    CONSTRAINT managed_scheduled_test_templates_max_results_check
        CHECK (max_results > 0)
);

INSERT INTO managed_scheduled_test_templates (
    template_key, platform, account_type, model_id, interval_minutes,
    shard_start_minute, shard_count, enabled, auto_recover,
    only_when_blocked, recover_scope, max_results
) VALUES (
    'openai-quota-recovery-v1', 'openai', 'oauth', 'gpt-5.6-terra', 30,
    1, 8, false, true, true, 'account_rate_limit_only', 20
) ON CONFLICT (template_key) DO NOTHING;

ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS managed_template_key VARCHAR(100),
    ADD COLUMN IF NOT EXISTS only_when_blocked BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS recover_scope VARCHAR(50) NOT NULL DEFAULT 'full';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'scheduled_test_plans_recover_scope_check'
    ) THEN
        ALTER TABLE scheduled_test_plans
            ADD CONSTRAINT scheduled_test_plans_recover_scope_check
            CHECK (recover_scope IN ('full', 'account_rate_limit_only'));
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_stp_account_managed_template_unique
    ON scheduled_test_plans(account_id, managed_template_key)
    WHERE managed_template_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_stp_managed_template
    ON scheduled_test_plans(managed_template_key)
    WHERE managed_template_key IS NOT NULL;

ALTER TABLE scheduled_test_results
    ADD COLUMN IF NOT EXISTS recovery_status VARCHAR(50) NOT NULL DEFAULT '';
