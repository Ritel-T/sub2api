package adminaccess

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBalanceRecoveryRoutePermissions(t *testing.T) {
	read := Policy{Rules: []Rule{{Permissions: []string{"accounts.read"}, GroupIDs: []int64{18}, AccountTypes: []string{"apikey"}}}}
	require.True(t, RouteAllowed(read, "GET", "/api/v1/admin/accounts/:id/balance-recovery-preview"))
	require.False(t, RouteAllowed(read, "POST", "/api/v1/admin/accounts/:id/balance-recover"))
	recover := Policy{Rules: []Rule{{Permissions: []string{"accounts.recover"}, GroupIDs: []int64{18}, AccountTypes: []string{"apikey"}}}}
	require.True(t, RouteAllowed(recover, "POST", "/api/v1/admin/accounts/:id/balance-recover"))
	require.False(t, RouteAllowed(recover, "GET", "/api/v1/admin/accounts/:id/balance-recovery-preview"))
}
