package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCachePreservesExcelBPSIsolation(t *testing.T) {
	extra := map[string]any{
		"openai_excel_bps":                  true,
		"openai_excel_bps_models":           []any{"gpt-6-astra", "gpt-6-sol"},
		service.ExcelBPSRequiredGroupIDsKey: []any{float64(16)},
		service.ExcelBPSRequiredModelsKey:   []any{"gpt-6-astra", "gpt-6-sol"},
	}
	filtered := filterSchedulerExtra(extra)
	require.Equal(t, extra, filtered)
	a := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: filtered}
	ordinary, degraded := int64(15), int64(16)
	require.False(t, a.IsModelAllowedInGroup(&ordinary, "gpt-6-astra"))
	require.True(t, a.IsModelAllowedInGroup(&degraded, "gpt-6-astra"))
	require.True(t, a.IsModelAllowedInGroup(&ordinary, "gpt-6-luna"))
	for _, key := range []string{service.ExcelBPSRequiredGroupIDsKey, service.ExcelBPSRequiredModelsKey} {
		require.False(t, isSchedulerNeutralExtraKey(key))
	}
}

func TestExcelBPSCooldownGenericExtraCannotMutateDeadline(t *testing.T) {
	extra := map[string]any{excelBPSCooldownResetKey: "stale", excelBPSCooldownReasonKey: "stale", "admin_setting": true}
	require.Equal(t, map[string]any{"admin_setting": true}, stripExcelBPSCooldownExtra(extra))
	require.Len(t, extra, 3, "do not mutate the caller's map")
}
