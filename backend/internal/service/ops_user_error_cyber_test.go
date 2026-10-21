package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapUserErrorCategoryCyber(t *testing.T) {
	require.Equal(t, "cyber", MapUserErrorCategory("request", "cyber_policy"))
	phases, types := CategoryToFilter("cyber")
	require.Equal(t, []string{"request"}, phases)
	require.Equal(t, []string{"cyber_policy"}, types)
}

func TestUserErrorSessionCategoriesPreserveHistoricalRequestType(t *testing.T) {
	for _, tc := range []struct{ errorType, category string }{
		{"cyber_policy", "cyber"},
		{"cyber_policy_session_blocked", "session_blocked"},
		{"cyber_session_identity_rejected", "session_identity"},
	} {
		t.Run(tc.errorType, func(t *testing.T) {
			historicalCyberType := int16(4)
			row := &OpsErrorLog{Phase: "request", Type: tc.errorType, RequestType: &historicalCyberType}
			out := ToUserErrorRequest(row)
			require.Equal(t, tc.category, out.Category)
			require.EqualValues(t, 4, *out.RequestType, "classification must not rewrite historical transport data")
			phases, types := CategoryToFilter(tc.category)
			require.Equal(t, []string{"request"}, phases)
			require.Equal(t, []string{tc.errorType}, types)
		})
	}
}
