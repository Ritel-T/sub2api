//go:build unit

package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestEmailDeliveryRecipientAliasesValidate(t *testing.T) {
	tests := []struct {
		name  string
		pairs []string
		want  map[string]string
		bad   bool
	}{
		{name: "disabled", want: map[string]string{}},
		{name: "normalize", pairs: []string{" RITELT.COM = Ritel.SI "}, want: map[string]string{"ritelt.com": "ritel.si"}},
		{name: "independent mappings", pairs: []string{"old.example=new.example", "second.example=other.example"}, want: map[string]string{"old.example": "new.example", "second.example": "other.example"}},
		{name: "wildcard", pairs: []string{"*.ritelt.com=ritel.si"}, bad: true},
		{name: "suffix pattern", pairs: []string{".ritelt.com=ritel.si"}, bad: true},
		{name: "address destination", pairs: []string{"ritelt.com=inbox@ritel.si"}, bad: true},
		{name: "header injection", pairs: []string{"ritelt.com=ritel.si\r\nBcc:other.example"}, bad: true},
		{name: "self", pairs: []string{"ritelt.com=ritelt.com"}, bad: true},
		{name: "duplicate case", pairs: []string{"ritelt.com=ritel.si", "RITELT.COM=other.example"}, bad: true},
		{name: "chain", pairs: []string{"old.example=new.example", "new.example=final.example"}, bad: true},
		{name: "cycle", pairs: []string{"old.example=new.example", "new.example=old.example"}, bad: true},
		{name: "no separator", pairs: []string{"ritelt.com"}, bad: true},
		{name: "empty destination", pairs: []string{"ritelt.com="}, bad: true},
		{name: "extra separator", pairs: []string{"ritelt.com=ritel.si=other.example"}, bad: true},
		{name: "URL", pairs: []string{"ritelt.com=https://ritel.si"}, bad: true},
		{name: "localhost", pairs: []string{"ritelt.com=localhost"}, bad: true},
		{name: "IP literal", pairs: []string{"ritelt.com=[127.0.0.1]"}, bad: true},
		{name: "IPv4", pairs: []string{"ritelt.com=127.0.0.1"}, bad: true},
		{name: "empty label", pairs: []string{"ritelt.com=ritel..si"}, bad: true},
		{name: "long label", pairs: []string{"ritelt.com=" + strings.Repeat("a", 64) + ".example"}, bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (EmailDeliveryConfig{RecipientDomainAliases: tt.pairs}).RecipientAliases()
			if tt.bad {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEmailDeliveryRecipientAliasesLoadFromYAML(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()
	viper.SetConfigType("yaml")
	require.NoError(t, viper.ReadConfig(strings.NewReader("email_delivery:\n  recipient_domain_aliases:\n    - ritelt.com=ritel.si\n")))
	var cfg Config
	require.NoError(t, viper.Unmarshal(&cfg))
	got, err := cfg.EmailDelivery.RecipientAliases()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"ritelt.com": "ritel.si"}, got)
}

func TestEmailDeliveryRecipientAliasesLoadFromEnvironment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setDefaults()
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	t.Setenv("EMAIL_DELIVERY_RECIPIENT_DOMAIN_ALIASES", "ritelt.com=ritel.si")
	var cfg Config
	require.NoError(t, viper.Unmarshal(&cfg))
	got, err := cfg.EmailDelivery.RecipientAliases()
	require.NoError(t, err)
	require.Equal(t, map[string]string{"ritelt.com": "ritel.si"}, got)
}

func TestEmailDeliveryInvalidAliasFailsConfigValidation(t *testing.T) {
	cfg := &Config{EmailDelivery: EmailDeliveryConfig{RecipientDomainAliases: []string{"*.ritelt.com=ritel.si"}}}
	require.ErrorContains(t, cfg.Validate(), "email_delivery.recipient_domain_aliases")
}
