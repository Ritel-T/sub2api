package config

import (
	"fmt"
	"net/url"
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

// ValidateAccountCenterRuntime prevents a central login from accepting a native
// session that cannot honor the published idle and optional-factor contracts.
func (c *Config) ValidateAccountCenterRuntime() error {
	if !c.AccountCenter.Enabled {
		return nil
	}
	if c.JWT.RefreshTokenExpireDays < 31 {
		return fmt.Errorf("jwt.refresh_token_expire_days must be at least 31 when account center is enabled")
	}
	if !c.Totp.EncryptionKeyConfigured || strings.TrimSpace(c.Totp.EncryptionKey) == "" {
		return fmt.Errorf("a stable explicit totp.encryption_key is required when account center is enabled")
	}
	if !c.WebAuthn.Enabled {
		return fmt.Errorf("webauthn.enabled must be true when account center is enabled")
	}
	origin, err := url.Parse(c.AccountCenter.PublicOrigin)
	if err != nil || !strings.EqualFold(strings.TrimSpace(c.WebAuthn.RPID), origin.Hostname()) {
		return fmt.Errorf("webauthn.rp_id must equal the account center hostname")
	}
	found := false
	for _, allowed := range c.WebAuthn.RPOrigins {
		if strings.TrimSpace(allowed) == c.AccountCenter.PublicOrigin {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("webauthn.rp_origins must include the exact account center public origin")
	}
	if c.Turnstile.StrictContext || len(c.Turnstile.ExpectedHostnames) > 0 {
		approved := false
		for _, hostname := range c.Turnstile.ExpectedHostnames {
			if strings.TrimSpace(hostname) == origin.Hostname() {
				approved = true
			}
		}
		if !approved {
			return fmt.Errorf("turnstile.expected_hostnames must include the exact account center hostname")
		}
	}
	return nil
}
