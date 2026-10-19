package handler

import (
	"net/http"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionOverflowUsagePolicyErrorContract(t *testing.T) {
	for _, reason := range []string{"CODEX_CLIENT_REQUIRED", "SUBAGENTS_BLOCKED"} {
		status, code, message, retryAfter := subscriptionBillingErrorDetails(infraerrors.Forbidden(reason, "usage policy denied"))
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, strings.ToLower(reason), code)
		require.Equal(t, "usage policy denied", message)
		require.Zero(t, retryAfter)
	}
}
