//go:build unit

package service

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_AffiliateFirstRechargeReadPaths(t *testing.T) {
	ctx := context.Background()
	for _, field := range []struct {
		key, jsonKey string
		get          func(*SettingService) float64
		max          float64
	}{
		{SettingKeyAffiliateFirstRechargeBonus, "affiliate_first_recharge_bonus", func(s *SettingService) float64 { return s.GetAffiliateFirstRechargeBonus(ctx) }, AffiliateFirstRechargeBonusMax},
		{SettingKeyAffiliateFirstRechargeMinAmount, "affiliate_first_recharge_min_amount", func(s *SettingService) float64 { return s.GetAffiliateFirstRechargeMinAmount(ctx) }, AffiliateFirstRechargeMinAmountMax},
	} {
		t.Run(field.key, func(t *testing.T) {
			for _, tc := range []struct {
				raw  string
				want float64
			}{
				{"", 0}, {"5", 5}, {"10", 10}, {"5.25", 5.25}, {"-5", 0}, {"bad", 0}, {"NaN", 0}, {"+Inf", 0},
				{strconv.FormatFloat(field.max+1, 'f', -1, 64), 0},
			} {
				t.Run(tc.raw, func(t *testing.T) {
					repo := &panelRateLimitSettingRepo{values: map[string]string{SettingKeyAffiliateEnabled: "true", field.key: tc.raw}}
					svc := NewSettingService(repo, &config.Config{})
					require.Equal(t, tc.want, field.get(svc))
					settings, err := svc.GetAllSettings(ctx)
					require.NoError(t, err)
					value := settings.AffiliateFirstRechargeBonus
					if field.key == SettingKeyAffiliateFirstRechargeMinAmount {
						value = settings.AffiliateFirstRechargeMinAmount
					}
					require.Equal(t, tc.want, value)
					public, err := svc.GetPublicSettings(ctx)
					require.NoError(t, err)
					value = public.AffiliateFirstRechargeBonus
					if field.key == SettingKeyAffiliateFirstRechargeMinAmount {
						value = public.AffiliateFirstRechargeMinAmount
					}
					require.Equal(t, tc.want, value)
					injected, err := svc.GetPublicSettingsForInjection(ctx)
					require.NoError(t, err)
					encoded, err := json.Marshal(injected)
					require.NoError(t, err)
					var data map[string]any
					require.NoError(t, json.Unmarshal(encoded, &data))
					require.Equal(t, tc.want, data[field.jsonKey])
				})
			}
		})
	}
}

func TestSettingService_AffiliateFirstRechargeWriteValidation(t *testing.T) {
	for _, tc := range []struct {
		value     float64
		wantError bool
	}{{5, false}, {-1, true}, {math.NaN(), true}, {math.Inf(1), true}} {
		for _, field := range []string{SettingKeyAffiliateFirstRechargeBonus, SettingKeyAffiliateFirstRechargeMinAmount} {
			t.Run(field+strconv.FormatFloat(tc.value, 'g', -1, 64), func(t *testing.T) {
				repo := &settingUpdateRepoStub{}
				svc := NewSettingService(repo, &config.Config{})
				settings := &SystemSettings{AffiliateSignupBonus: 5, AffiliateRebateRate: 10}
				if field == SettingKeyAffiliateFirstRechargeBonus {
					settings.AffiliateFirstRechargeBonus = tc.value
				} else {
					settings.AffiliateFirstRechargeMinAmount = tc.value
				}
				err := svc.UpdateSettings(context.Background(), settings)
				if tc.wantError {
					require.Error(t, err)
					require.Empty(t, repo.updates)
					return
				}
				require.NoError(t, err)
				require.Equal(t, "5.00000000", repo.updates[field])
				require.Equal(t, "5.00000000", repo.updates[SettingKeyAffiliateSignupBonus])
				require.Equal(t, "10.00000000", repo.updates[SettingKeyAffiliateRebateRate])
			})
		}
	}
}
