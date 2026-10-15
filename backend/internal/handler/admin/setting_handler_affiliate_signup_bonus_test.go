//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsAffiliateSignupBonusRoundTripsAndKeepsUnsentValue(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyDefaultBalance: "0",
	})

	rec := doUpdateSettings(t, h, map[string]any{"affiliate_signup_bonus": 5}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "5.00000000", repo.values[service.SettingKeyAffiliateSignupBonus])
	require.Equal(t, "0", repo.values[service.SettingKeyDefaultBalance])
	var payload struct {
		Data struct {
			AffiliateSignupBonus float64 `json:"affiliate_signup_bonus"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, 5.0, payload.Data.AffiliateSignupBonus)

	rec = doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "5.00000000", repo.values[service.SettingKeyAffiliateSignupBonus])

	rec = doUpdateSettings(t, h, map[string]any{"affiliate_signup_bonus": 0}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "0.00000000", repo.values[service.SettingKeyAffiliateSignupBonus])
}

func TestUpdateSettingsAffiliateSignupBonusNormalizesBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		amount float64
		want   string
	}{
		{"negative", -1, "0.00000000"},
		{"above maximum", 1001, "1000.00000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, nil)
			rec := doUpdateSettings(t, h, map[string]any{"affiliate_signup_bonus": tc.amount}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, tc.want, repo.values[service.SettingKeyAffiliateSignupBonus])
		})
	}
}
