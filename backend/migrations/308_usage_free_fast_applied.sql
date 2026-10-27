-- Preserve historical uncertainty: never infer past Free Fast pricing from a current group policy.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS free_fast_applied BOOLEAN;
