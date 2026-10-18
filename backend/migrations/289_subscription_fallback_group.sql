-- Optional subscription quota overflow destination. No default activates routing.
-- Keep an independent ID rather than repurposing either Claude fallback setting.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS subscription_fallback_group_id BIGINT;
COMMENT ON COLUMN groups.subscription_fallback_group_id IS 'Optional same-platform active standard group for exhausted subscription quotas; revalidated at request time';
