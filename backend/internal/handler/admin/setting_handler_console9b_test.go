package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// The feature partition sends the switch only; URL/key drafts belong to gateway.
func TestConsole9bPrismPartialEnableUsesStoredCredentials(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyPrismBrowserEnabled:     "false",
		service.SettingKeyPrismBrowserBaseURL:     "http://saved-adapter:8319/v1",
		service.SettingKeyPrismBrowserAPIKey:      strings.Repeat("k", 32),
		service.SettingKeySiteName:                "Preserved site",
		service.SettingKeySMTPHost:                "smtp.example.test",
		service.SettingKeySMTPPassword:            "stored-smtp-secret",
		service.SettingKeyGoogleOAuthClientSecret: "stored-google-secret",
	})
	before := make(map[string]string, len(repo.values))
	for k, v := range repo.values {
		before[k] = v
	}
	rec := doUpdateSettings(t, h, map[string]any{"prism_browser_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyPrismBrowserEnabled])
	for k, v := range before {
		if k != service.SettingKeyPrismBrowserEnabled {
			require.Equal(t, v, repo.values[k], k)
		}
	}
	require.NotContains(t, rec.Body.String(), strings.Repeat("k", 32))
	require.NotContains(t, rec.Body.String(), "stored-smtp-secret")
	require.NotContains(t, rec.Body.String(), "stored-google-secret")
	// Any later save of the feature partition also includes the still-enabled switch.
	rec = doUpdateSettings(t, h, map[string]any{"prism_browser_enabled": true, "excel_bps_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "http://saved-adapter:8319/v1", repo.values[service.SettingKeyPrismBrowserBaseURL])
}

func TestConsole9bPrismPartialEnableStillValidatesExplicitURLAndKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]any
		key     string
		status  int
	}{
		{"explicit empty URL", map[string]any{"prism_browser_enabled": true, "prism_browser_base_url": ""}, strings.Repeat("k", 32), http.StatusBadRequest},
		{"explicit short key", map[string]any{"prism_browser_enabled": true, "prism_browser_api_key": "short"}, strings.Repeat("k", 32), http.StatusBadRequest},
		{"missing stored key", map[string]any{"prism_browser_enabled": true}, "", http.StatusBadRequest},
		{"gateway replaces URL and key", map[string]any{"prism_browser_enabled": true, "prism_browser_base_url": "http://new-adapter:8319/v1", "prism_browser_api_key": strings.Repeat("n", 32)}, strings.Repeat("k", 32), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{
				service.SettingKeyPrismBrowserEnabled: "false",
				service.SettingKeyPrismBrowserBaseURL: "http://saved-adapter:8319/v1",
				service.SettingKeyPrismBrowserAPIKey:  tc.key,
			})
			rec := doUpdateSettings(t, h, tc.payload, nil)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status != http.StatusOK {
				require.Equal(t, "false", repo.values[service.SettingKeyPrismBrowserEnabled])
				require.Equal(t, tc.key, repo.values[service.SettingKeyPrismBrowserAPIKey])
			} else {
				require.Equal(t, "http://new-adapter:8319/v1", repo.values[service.SettingKeyPrismBrowserBaseURL])
				require.Equal(t, strings.Repeat("n", 32), repo.values[service.SettingKeyPrismBrowserAPIKey])
			}
		})
	}
}

func TestConsole9bPartialEmailSavePreservesOtherPartitionsAndSecret(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName:                "Unchanged site",
		service.SettingKeySMTPPassword:            "stored-smtp-secret",
		service.SettingKeyGoogleOAuthClientSecret: "stored-google-secret",
		service.SettingKeyPrismBrowserAPIKey:      strings.Repeat("k", 32),
	})
	rec := doUpdateSettings(t, h, map[string]any{"smtp_host": "smtp.new.test", "smtp_password": "", "smtp_from_email": "sender@example.test"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "smtp.new.test", repo.values[service.SettingKeySMTPHost])
	require.Equal(t, "sender@example.test", repo.values[service.SettingKeySMTPFrom])
	require.Equal(t, "stored-smtp-secret", repo.values[service.SettingKeySMTPPassword])
	require.Equal(t, "stored-google-secret", repo.values[service.SettingKeyGoogleOAuthClientSecret])
	require.Equal(t, "Unchanged site", repo.values[service.SettingKeySiteName])
	rec = doUpdateSettings(t, h, map[string]any{"smtp_password": "replacement-smtp-secret"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "replacement-smtp-secret", repo.values[service.SettingKeySMTPPassword])
	require.NotContains(t, rec.Body.String(), "replacement-smtp-secret")
	require.Equal(t, "stored-google-secret", repo.values[service.SettingKeyGoogleOAuthClientSecret])
}

func TestConsole9bFeatureAndPaymentPartitionsKeepOmittedConfig(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingPaymentEnabled:                         "true",
		service.SettingMinRechargeAmount:                      "12.50",
		service.SettingProductNamePrefix:                      "Saved prefix",
		service.SettingRechargeFeeRate:                        "6.00",
		service.SettingKeySMTPPassword:                        "stored-smtp-secret",
		service.SettingKeyAuthSourceDefaultEmailBalance:       "12.50000000",
		service.SettingKeyAuthSourceDefaultEmailConcurrency:   "7",
		service.SettingKeyAuthSourceDefaultEmailSubscriptions: `[{"group_id":11,"validity_days":30}]`,
	})
	h.paymentConfigService = service.NewPaymentConfigService(nil, repo, nil)
	before := make(map[string]string, len(repo.values))
	for k, v := range repo.values {
		before[k] = v
	}
	rec := doUpdateSettings(t, h, map[string]any{"payment_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "false", repo.values[service.SettingPaymentEnabled])
	for k, v := range before {
		if k != service.SettingPaymentEnabled {
			require.Equal(t, v, repo.values[k], k)
		}
	}
	rec = doUpdateSettings(t, h, map[string]any{"payment_help_text": "New help"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "New help", repo.values[service.SettingHelpText])
	require.Equal(t, "false", repo.values[service.SettingPaymentEnabled])
	require.Equal(t, "12.50", repo.values[service.SettingMinRechargeAmount])
	require.Equal(t, "Saved prefix", repo.values[service.SettingProductNamePrefix])
	require.Equal(t, "6.00", repo.values[service.SettingRechargeFeeRate])
	require.Equal(t, "12.50000000", repo.values[service.SettingKeyAuthSourceDefaultEmailBalance])
	require.Equal(t, "stored-smtp-secret", repo.values[service.SettingKeySMTPPassword])
}
