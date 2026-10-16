package config

import (
	"errors"
	"net"
	"strings"
)

// EmailDeliveryConfig affects only SMTP delivery, never login or account identity.
type EmailDeliveryConfig struct {
	// Exact source=destination domain pairs. Empty disables rewriting.
	// Only use for recipient domains whose mailboxes share the same owner.
	RecipientDomainAliases []string `mapstructure:"recipient_domain_aliases"`
}

// RecipientAliases validates one-hop, exact-domain mappings; wildcard aliases,
// duplicate sources, chained mappings and cycles fail startup.
func (c EmailDeliveryConfig) RecipientAliases() (map[string]string, error) {
	aliases := make(map[string]string, len(c.RecipientDomainAliases))
	if len(c.RecipientDomainAliases) > 32 {
		return nil, errors.New("at most 32 recipient domain aliases are allowed")
	}
	for _, pair := range c.RecipientDomainAliases {
		source, destination, ok := strings.Cut(pair, "=")
		source = strings.ToLower(strings.TrimSpace(source))
		destination = strings.ToLower(strings.TrimSpace(destination))
		if !ok || !validRecipientAliasDomain(source) || !validRecipientAliasDomain(destination) || source == destination {
			return nil, errors.New("recipient domain alias must be two distinct DNS domains separated by '='")
		}
		if _, exists := aliases[source]; exists {
			return nil, errors.New("duplicate recipient domain alias source")
		}
		aliases[source] = destination
	}
	for _, destination := range aliases {
		if _, chained := aliases[destination]; chained {
			return nil, errors.New("chained or cyclic recipient domain aliases are not allowed")
		}
	}
	return aliases, nil
}

func validRecipientAliasDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, b := range []byte(label) {
			if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-') {
				return false
			}
		}
	}
	return true
}
