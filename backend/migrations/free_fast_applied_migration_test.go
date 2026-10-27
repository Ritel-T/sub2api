package migrations

import (
	"strings"
	"testing"
)

func TestFreeFastAppliedMigrationPreservesHistoricalUnknown(t *testing.T) {
	data, err := FS.ReadFile("308_usage_free_fast_applied.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(data))
	if !strings.Contains(sql, "add column if not exists free_fast_applied boolean") {
		t.Fatal("missing additive nullable snapshot column")
	}
	for _, forbidden := range []string{"default", "not null", "update usage_logs", "delete", "drop"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration must retain historical uncertainty: %q", forbidden)
		}
	}
}
