package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactAuditBody_AccountSecurityCredentials(t *testing.T) {
	raw := []byte(`{"old_password":"old-canary","new_password":"new-canary","password":"pwd-canary","verify_code":"code-canary","data":{"access_token":"access-canary","refresh_token":"refresh-canary"}}`)
	var result struct {
		OldPassword string            `json:"old_password"`
		NewPassword string            `json:"new_password"`
		Password    string            `json:"password"`
		VerifyCode  string            `json:"verify_code"`
		Data        map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(RedactAuditBody(raw, "application/json")), &result))
	require.Equal(t, "***", result.OldPassword)
	require.Equal(t, "***", result.NewPassword)
	require.Equal(t, "***", result.Password)
	require.Equal(t, "***", result.VerifyCode)
	require.Equal(t, "***", result.Data["access_token"])
	require.Equal(t, "***", result.Data["refresh_token"])
}
