-- Only the new mini-program provider is affected; historical provider orders
-- and empty references are excluded. No existing order or financial row changes.
CREATE UNIQUE INDEX IF NOT EXISTS paymentorder_gelv_external_order
    ON payment_orders (provider_instance_id, payment_trade_no)
    WHERE provider_key = 'gelv' AND payment_trade_no <> '';

-- Also reject a platform payment incorrectly linked to two upstream source
-- orders. The receipt identity is bound atomically with PAID and final credit.
CREATE UNIQUE INDEX IF NOT EXISTS paymentorder_gelv_platform_payment
    ON payment_orders (provider_instance_id, (provider_snapshot->>'gelv_wx_order_id'))
    WHERE provider_key = 'gelv' AND provider_snapshot->>'gelv_wx_order_id' <> '';
