//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsAffiliateFirstRechargeRoundTripAndKeepUnsent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyAffiliateSignupBonus: "5", service.SettingKeyAffiliateRebateRate: "10",
	})
	rec := doUpdateSettings(t, h, map[string]any{"affiliate_first_recharge_bonus": 5, "affiliate_first_recharge_min_amount": 10}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "5.00000000", repo.values[service.SettingKeyAffiliateFirstRechargeBonus])
	require.Equal(t, "10.00000000", repo.values[service.SettingKeyAffiliateFirstRechargeMinAmount])
	var payload struct {
		Data struct {
			Bonus   float64 `json:"affiliate_first_recharge_bonus"`
			Minimum float64 `json:"affiliate_first_recharge_min_amount"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, 5.0, payload.Data.Bonus)
	require.Equal(t, 10.0, payload.Data.Minimum)
	rec = doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "5.00000000", repo.values[service.SettingKeyAffiliateFirstRechargeBonus])
	require.Equal(t, "10.00000000", repo.values[service.SettingKeyAffiliateFirstRechargeMinAmount])
	require.Equal(t, "5.00000000", repo.values[service.SettingKeyAffiliateSignupBonus])
	require.Equal(t, "10.00000000", repo.values[service.SettingKeyAffiliateRebateRate])
	rec = doUpdateSettings(t, h, map[string]any{"affiliate_first_recharge_bonus": 0}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "0.00000000", repo.values[service.SettingKeyAffiliateFirstRechargeBonus])
	require.Equal(t, "10.00000000", repo.values[service.SettingKeyAffiliateFirstRechargeMinAmount])
}

func TestUpdateSettingsAffiliateFirstRechargeRejectsNegativeAndCapsBounds(t *testing.T) {
	for _, field := range []struct {
		key     string
		maximum float64
	}{
		{service.SettingKeyAffiliateFirstRechargeBonus, service.AffiliateFirstRechargeBonusMax},
		{service.SettingKeyAffiliateFirstRechargeMinAmount, service.AffiliateFirstRechargeMinAmountMax},
	} {
		t.Run(field.key, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{field.key: "5"})
			rec := doUpdateSettings(t, h, map[string]any{field.key: -1}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, "5", repo.values[field.key])
			rec = doUpdateSettings(t, h, map[string]any{field.key: field.maximum + 1}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, strconv.FormatFloat(field.maximum, 'f', 8, 64), repo.values[field.key])
		})
	}
}
