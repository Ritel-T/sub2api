-- [OpusClaw Patch] One-time migration: convert disabled accounts to active
-- Run during deployment of auto-scheduling feature
-- Database stores 'disabled' (domain constant), API layer uses 'inactive'
UPDATE accounts SET status = 'active' WHERE status = 'disabled';
