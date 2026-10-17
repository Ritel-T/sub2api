package service

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type forkTimezoneCompatibilitySettings struct {
	SettingRepository
	values map[string]string
}

func (r *forkTimezoneCompatibilitySettings) GetValue(_ context.Context, key string) (string, error) {
	value, exists := r.values[key]
	if !exists {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *forkTimezoneCompatibilitySettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, key := range keys {
		if value, exists := r.values[key]; exists {
			values[key] = value
		}
	}
	return values, nil
}

func (r *forkTimezoneCompatibilitySettings) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestForkRequestTimezoneUpgradeRetainsManualBinding(t *testing.T) {
	for _, missingAuto := range []bool{true, false} {
		name := "empty auto setting"
		if missingAuto {
			name = "missing auto setting"
		}
		t.Run(name, func(t *testing.T) {
			repo := &forkTimezoneCompatibilitySettings{values: map[string]string{
				SettingKeyRegistrationEnabled:          "true",
				SettingKeyOpenAIRequestTimezoneEnabled: "true",
			}}
			if !missingAuto {
				repo.values[SettingKeyOpenAIRequestTimezoneAuto] = ""
			}
			settings := NewSettingService(repo, &config.Config{})
			require.NoError(t, settings.InitializeDefaultSettings(context.Background()))
			require.True(t, settings.GetOpenAIRequestTimezoneEnabled(context.Background(), false))
			require.False(t, settings.GetOpenAIRequestTimezoneAuto(context.Background(), true), "an absent new setting must not opt an existing deployment into automatic mode")
			require.False(t, settings.parseSettings(repo.values).OpenAIRequestTimezoneAuto)

			svc := &OpenAIGatewayService{settingService: settings}
			account := &Account{Platform: PlatformOpenAI, Extra: map[string]any{openAIRequestTimezoneExtraKey: "Asia/Tokyo"}}
			body := localeTestBody([]string{"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"}, nil)
			for _, transport := range []string{"http", "ws"} {
				normalized := svc.normalizeRequestTimezone(context.Background(), account, body, transport)
				require.Contains(t, gjson.GetBytes(normalized, "input.0.content.0.text").String(), "Asia/Tokyo")
				require.Equal(t, "Asia/Tokyo", gjson.GetBytes(normalized, "tools.0.user_location.timezone").String())
			}
			source := http.Header{"X-Timezone": []string{"Asia/Shanghai"}}
			headers := http.Header{}
			svc.requestTimezoneHeaders(context.Background(), account, source, headers)
			require.Equal(t, "Asia/Tokyo", headers.Get("X-Timezone"))

			req, err := http.NewRequestWithContext(svc.withRequestTimezoneEgress(context.Background(), account), http.MethodPost, "https://example.invalid/responses", bytes.NewReader(body))
			require.NoError(t, err)
			bound, err := BindOpenAIRequestTimezone(req, "")
			require.NoError(t, err)
			require.Same(t, req, bound, "automatic transport binding must remain inactive after a legacy upgrade")
			require.Equal(t, "Asia/Tokyo", account.Extra[openAIRequestTimezoneExtraKey], "request handling must preserve the persisted account mapping")
		})
	}
}

func TestForkRequestTimezoneDefaultsRequireExplicitOptIn(t *testing.T) {
	repo := &forkTimezoneCompatibilitySettings{values: map[string]string{}}
	settings := NewSettingService(repo, &config.Config{})
	require.NoError(t, settings.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "false", repo.values[SettingKeyOpenAIRequestTimezoneEnabled])
	require.Equal(t, "false", repo.values[SettingKeyOpenAIRequestTimezoneAuto])

	for _, value := range []string{"", "false", "invalid", "true"} {
		t.Run(value, func(t *testing.T) {
			entries := map[string]string{
				SettingKeyOpenAIRequestTimezoneEnabled: value,
				SettingKeyOpenAIRequestTimezoneAuto:    value,
			}
			svc := NewSettingService(&forkTimezoneCompatibilitySettings{values: entries}, &config.Config{})
			want := value == "true"
			parsed := svc.parseSettings(entries)
			require.Equal(t, want, parsed.OpenAIRequestTimezoneEnabled)
			require.Equal(t, want, parsed.OpenAIRequestTimezoneAuto)
			require.Equal(t, want, svc.GetOpenAIRequestTimezoneEnabled(context.Background(), true))
			require.Equal(t, want, svc.GetOpenAIRequestTimezoneAuto(context.Background(), true))
		})
	}

	svc := NewSettingService(&forkTimezoneCompatibilitySettings{values: map[string]string{}}, &config.Config{})
	require.False(t, svc.parseSettings(map[string]string{}).OpenAIRequestTimezoneEnabled)
	require.False(t, svc.GetOpenAIRequestTimezoneEnabled(context.Background(), true))
	require.False(t, svc.GetOpenAIRequestTimezoneAuto(context.Background(), true))
}
