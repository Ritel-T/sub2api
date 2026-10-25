package config

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAccountCenterConfig(t *testing.T) {
	require.NoError(t, (AccountCenterConfig{}).Validate())
	for _, secret := range []string{"", strings.Repeat("a", 42), strings.Repeat("a", 43) + "\n", strings.Repeat("a", 257), strings.Repeat("密", 43)} {
		require.Error(t, (AccountCenterConfig{Enabled: true, BridgeSecret: secret}).Validate())
	}
	require.NoError(t, (AccountCenterConfig{Enabled: true, BridgeSecret: strings.Repeat("a", 43)}).Validate())
}
