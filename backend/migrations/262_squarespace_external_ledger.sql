-- External payment proof and recovery ledger. No customer email, names or arbitrary checkout fields.
CREATE TABLE payment_external_orders (
 id BIGSERIAL PRIMARY KEY,
 provider_key VARCHAR(30) NOT NULL,
 website_id VARCHAR(128) NOT NULL,
 external_order_id VARCHAR(128) NOT NULL,
 order_number VARCHAR(64) NOT NULL DEFAULT '',
 binding_method VARCHAR(30) NOT NULL DEFAULT 'reference',
 quote_hash VARCHAR(64) NOT NULL DEFAULT '',
 local_order_id BIGINT NULL REFERENCES payment_orders(id) ON DELETE RESTRICT,
 checkout_reference VARCHAR(64) NOT NULL DEFAULT '',
 currency VARCHAR(3) NOT NULL DEFAULT '',
 total_minor BIGINT NOT NULL DEFAULT 0,
 refunded_minor BIGINT NOT NULL DEFAULT 0,
 credited_usd_units BIGINT NOT NULL DEFAULT 0,
 refund_target_usd_units BIGINT NOT NULL DEFAULT 0,
 recovered_usd_units BIGINT NOT NULL DEFAULT 0,
 debt_usd_units BIGINT NOT NULL DEFAULT 0,
 payment_state VARCHAR(30) NOT NULL DEFAULT 'UNKNOWN',
 status VARCHAR(30) NOT NULL DEFAULT 'OBSERVED',
 anomaly_code VARCHAR(64) NOT NULL DEFAULT '',
 paid_at TIMESTAMPTZ NULL,
 provider_modified_at TIMESTAMPTZ NULL,
 last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(total_minor >= 0 AND refunded_minor >= 0 AND credited_usd_units >= 0 AND refund_target_usd_units >= 0 AND recovered_usd_units >= 0 AND debt_usd_units >= 0)
);
CREATE UNIQUE INDEX paymentexternalorder_provider_key_website_id_external_order_id ON payment_external_orders(provider_key, website_id, external_order_id);
CREATE INDEX paymentexternalorder_provider_key_website_id_order_number ON payment_external_orders(provider_key, website_id, order_number);
CREATE UNIQUE INDEX paymentexternalorder_local_order_id ON payment_external_orders(local_order_id);
CREATE INDEX paymentexternalorder_provider_key_website_id_status ON payment_external_orders(provider_key, website_id, status);
CREATE INDEX paymentexternalorder_anomaly_code ON payment_external_orders(anomaly_code);

CREATE TABLE payment_external_payments (
 id BIGSERIAL PRIMARY KEY,
 provider_key VARCHAR(30) NOT NULL,
 website_id VARCHAR(128) NOT NULL,
 payment_id VARCHAR(128) NOT NULL,
 external_order_ledger_id BIGINT NOT NULL REFERENCES payment_external_orders(id) ON DELETE RESTRICT,
 currency VARCHAR(3) NOT NULL,
 amount_minor BIGINT NOT NULL,
 refunded_minor BIGINT NOT NULL DEFAULT 0,
 paid_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(amount_minor >= 0 AND refunded_minor >= 0 AND refunded_minor <= amount_minor)
);
CREATE UNIQUE INDEX paymentexternalpayment_provider_key_website_id_payment_id ON payment_external_payments(provider_key, website_id, payment_id);
CREATE INDEX paymentexternalpayment_external_order_ledger_id ON payment_external_payments(external_order_ledger_id);

CREATE TABLE payment_external_refund_journals (
 id BIGSERIAL PRIMARY KEY,
 external_order_ledger_id BIGINT NOT NULL REFERENCES payment_external_orders(id) ON DELETE RESTRICT,
 cumulative_refund_minor BIGINT NOT NULL,
 delta_refund_minor BIGINT NOT NULL,
 target_usd_units BIGINT NOT NULL,
 delta_target_usd_units BIGINT NOT NULL,
 recovered_usd_units BIGINT NOT NULL DEFAULT 0,
 debt_usd_units BIGINT NOT NULL DEFAULT 0,
 status VARCHAR(30) NOT NULL DEFAULT 'RECORDED',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(cumulative_refund_minor >= 0 AND delta_refund_minor >= 0 AND target_usd_units >= 0 AND delta_target_usd_units >= 0 AND recovered_usd_units >= 0 AND debt_usd_units >= 0)
);
CREATE UNIQUE INDEX paymentexternalrefundjournal_order_cumulative ON payment_external_refund_journals(external_order_ledger_id, cumulative_refund_minor);
CREATE INDEX paymentexternalrefundjournal_status ON payment_external_refund_journals(status);

CREATE TABLE payment_sync_states (
 id BIGSERIAL PRIMARY KEY,
 provider_key VARCHAR(30) NOT NULL,
 website_id VARCHAR(128) NOT NULL,
 oauth_client_id VARCHAR(200) NOT NULL DEFAULT '',
 encrypted_oauth_credentials TEXT NOT NULL DEFAULT '',
 encrypted_oauth_tokens TEXT NOT NULL DEFAULT '',
 token_version BIGINT NOT NULL DEFAULT 0,
 rotation_phase VARCHAR(30) NOT NULL DEFAULT 'idle',
 rotation_started_at TIMESTAMPTZ NULL,
 next_cursor TEXT NOT NULL DEFAULT '',
 window_start TIMESTAMPTZ NULL,
 window_end TIMESTAMPTZ NULL,
 last_synced_at TIMESTAMPTZ NULL,
 retry_at TIMESTAMPTZ NULL,
 last_error_code VARCHAR(64) NOT NULL DEFAULT '',
 lease_owner VARCHAR(64) NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX paymentsyncstate_provider_key_website_id ON payment_sync_states(provider_key, website_id);
