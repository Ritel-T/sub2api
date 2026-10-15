//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_AffiliateSignupBonusReadPaths(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		raw  string
		want float64
	}{
		{"missing", "", 0},
		{"enabled", "5", 5},
		{"fractional", "5.25", 5.25},
		{"negative", "-5", 0},
		{"invalid", "bad", 0},
		{"nan", "NaN", 0},
		{"infinity", "+Inf", 0},
		{"above maximum", "1001", AffiliateSignupBonusMax},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &panelRateLimitSettingRepo{values: map[string]string{
				SettingKeyAffiliateEnabled: "true",
			}}
			if tc.raw != "" {
				repo.values[SettingKeyAffiliateSignupBonus] = tc.raw
			}
			svc := NewSettingService(repo, &config.Config{})
			require.Equal(t, tc.want, svc.GetAffiliateSignupBonus(ctx))

			settings, err := svc.GetAllSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, settings.AffiliateSignupBonus)

			public, err := svc.GetPublicSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, public.AffiliateSignupBonus)

			injected, err := svc.GetPublicSettingsForInjection(ctx)
			require.NoError(t, err)
			encoded, err := json.Marshal(injected)
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, json.Unmarshal(encoded, &data))
			require.Equal(t, tc.want, data["affiliate_signup_bonus"])
		})
	}

	svc := NewSettingService(&panelRateLimitSettingRepo{getValueErr: errors.New("unavailable")}, &config.Config{})
	require.Zero(t, svc.GetAffiliateSignupBonus(ctx))
}

func TestSettingService_AffiliateSignupBonusWriteBounds(t *testing.T) {
	for _, tc := range []struct {
		value float64
		want  float64
	}{
		{0, 0}, {5, 5}, {5.25, 5.25}, {-1, 0},
		{1001, AffiliateSignupBonusMax}, {math.NaN(), 0}, {math.Inf(1), 0},
	} {
		t.Run(strconv.FormatFloat(tc.value, 'g', -1, 64), func(t *testing.T) {
			repo := &settingUpdateRepoStub{}
			svc := NewSettingService(repo, &config.Config{})
			require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{
				AffiliateSignupBonus: tc.value,
				DefaultBalance:       2,
			}))
			require.Equal(t, strconv.FormatFloat(tc.want, 'f', 8, 64), repo.updates[SettingKeyAffiliateSignupBonus])
			require.Equal(t, "2.00000000", repo.updates[SettingKeyDefaultBalance])
		})
	}
}
