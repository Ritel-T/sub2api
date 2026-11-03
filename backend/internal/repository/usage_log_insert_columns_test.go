package repository

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// usageLogInsertColumnIndexForTest derives positions from an actual INSERT input
// column list, so additive columns cannot silently shift unrelated assertions.
func usageLogInsertColumnIndexForTest(t *testing.T, column string) int {
	t.Helper()
	query, _ := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepareUsageLogInsert(&service.UsageLog{})})
	match := regexp.MustCompile(`(?s)WITH input \((.*?)\) AS \(VALUES`).FindStringSubmatch(query)
	require.Len(t, match, 2, "unrecognised best-effort INSERT input columns")
	columns := strings.Split(match[1], ",")
	require.Len(t, columns, len(usageLogInsertArgTypes))
	found := -1
	for i, name := range columns {
		if strings.TrimSpace(name) == column {
			require.Equal(t, -1, found, "duplicate INSERT column %s", column)
			found = i
		}
	}
	require.NotEqual(t, -1, found, "INSERT column %s is absent", column)
	return found
}
