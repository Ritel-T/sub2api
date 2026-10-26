package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// AccountCenterConfig is disabled by default. Its dedicated read-only bridge
// credential must never be reused as a JWT secret, admin API key or browser token.
type AccountCenterConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	BridgeSecret string `mapstructure:"bridge_secret"`
	// PublicOrigin is the fixed account UI origin used only for password reset emails.
	PublicOrigin string `mapstructure:"public_origin"`
}

func (c AccountCenterConfig) Validate() error {
	if c.Enabled && c.PublicOrigin == "" {
		return fmt.Errorf("account_center.public_origin is required when account center is enabled")
	}
	if c.PublicOrigin != "" {
		if err := urlvalidator.ValidateHTTPSOrigin(c.PublicOrigin); err != nil ||
			!strings.HasPrefix(c.PublicOrigin, "https://") || strings.TrimSpace(c.PublicOrigin) != c.PublicOrigin || strings.HasSuffix(c.PublicOrigin, "/") {
			return fmt.Errorf("account_center.public_origin must be an exact HTTPS origin without credentials, path, query or fragment")
		}
	}
	if c.Enabled && !regexp.MustCompile(`^[A-Za-z0-9_-]{43,256}$`).MatchString(c.BridgeSecret) {
		return fmt.Errorf("account_center.bridge_secret must be a dedicated random base64url credential of at least 32 bytes")
	}
	return nil
}
