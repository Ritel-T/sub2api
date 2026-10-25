package config

import (
	"fmt"
	"regexp"
)

// AccountCenterConfig is disabled by default. Its dedicated read-only bridge
// credential must never be reused as a JWT secret, admin API key or browser token.
type AccountCenterConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	BridgeSecret string `mapstructure:"bridge_secret"`
}

func (c AccountCenterConfig) Validate() error {
	if c.Enabled && !regexp.MustCompile(`^[A-Za-z0-9_-]{43,256}$`).MatchString(c.BridgeSecret) {
		return fmt.Errorf("account_center.bridge_secret must be a dedicated random base64url credential of at least 32 bytes")
	}
	return nil
}
