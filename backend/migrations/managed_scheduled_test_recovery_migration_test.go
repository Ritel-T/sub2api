package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedScheduledTestRecoveryMigration(t *testing.T) {
	content, err := FS.ReadFile("233_managed_scheduled_test_recovery.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS managed_scheduled_test_templates")
	require.Contains(t, sql, "'openai-quota-recovery-v1', 'openai', 'oauth', 'gpt-5.6-terra', 30")
	require.Contains(t, sql, "enabled BOOLEAN NOT NULL DEFAULT false")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS managed_template_key VARCHAR(100)")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS only_when_blocked BOOLEAN NOT NULL DEFAULT false")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS recover_scope VARCHAR(50) NOT NULL DEFAULT 'full'")
	require.Contains(t, sql, "CHECK (recover_scope IN ('full', 'account_rate_limit_only'))")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS idx_stp_account_managed_template_unique")
	require.Contains(t, sql, "WHERE managed_template_key IS NOT NULL")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS recovery_status VARCHAR(50) NOT NULL DEFAULT ''")
}
