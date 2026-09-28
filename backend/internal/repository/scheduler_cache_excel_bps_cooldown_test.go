package repository

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerMetadataAccountKeepsExcelBPSCooldownAndToolOmission(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(fmt.Sprintf("omit_%t", omit), func(t *testing.T) {
			account := service.Account{ID: 27, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Extra: map[string]any{
					"openai_excel_bps":                        true,
					excelBPSCooldownResetKey:                  "2026-09-28T03:00:00.123456789Z",
					excelBPSCooldownReasonKey:                 "quota_exhausted",
					"openai_excel_bps_omit_unsupported_tools": omit,
					"unrelated_large_payload":                 "drop",
				}}
			data, err := json.Marshal(buildSchedulerMetadataAccount(account))
			require.NoError(t, err)
			var restored service.Account
			require.NoError(t, json.Unmarshal(data, &restored))
			for _, key := range []string{excelBPSCooldownResetKey, excelBPSCooldownReasonKey, "openai_excel_bps_omit_unsupported_tools"} {
				require.Equal(t, account.Extra[key], restored.Extra[key], key)
			}
			require.NotContains(t, restored.Extra, "unrelated_large_payload")
			require.Contains(t, account.Extra, "unrelated_large_payload", "projection leaves its input intact")
		})
	}
}
