//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type firstRechargePolicyRepo struct {
	AffiliateRepository
	got AffiliateFirstRecharge
	err error
}

func (r *firstRechargePolicyRepo) RecordFirstRecharge(_ context.Context, recharge AffiliateFirstRecharge) (*AffiliateFirstRechargeResult, error) {
	r.got = recharge
	return nil, r.err
}

func TestAffiliateFirstRechargePolicy_StrictAmountsAndTransaction(t *testing.T) {
	for _, raw := range []string{"NaN", "+Inf", "-1", "not-money", "1000.00000001"} {
		t.Run(raw, func(t *testing.T) {
			_, err := strictFirstRechargeSetting(raw, AffiliateFirstRechargeBonusMax)
			require.Error(t, err)
		})
	}
	for _, raw := range []string{"", "0", "5", "1000"} {
		_, err := strictFirstRechargeSetting(raw, AffiliateFirstRechargeBonusMax)
		require.NoError(t, err)
	}
	repo := &firstRechargePolicyRepo{}
	settings := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled: "true", SettingKeyAffiliateFirstRechargeBonus: "5", SettingKeyAffiliateFirstRechargeMinAmount: "10",
	}}, &config.Config{})
	svc := NewAffiliateService(repo, settings, nil, nil)
	ctx := dbent.NewTxContext(context.Background(), &dbent.Tx{})
	_, err := svc.recordFirstRecharge(context.Background(), AffiliateFirstRecharge{})
	require.Error(t, err)
	input := AffiliateFirstRecharge{InviteeUserID: 2, RedeemCodeID: 7, Amount: 10}
	_, err = svc.recordFirstRecharge(ctx, input)
	require.NoError(t, err)
	require.True(t, repo.got.Enabled)
	require.Equal(t, 5.0, repo.got.Bonus)
	require.Equal(t, 10.0, repo.got.MinimumAmount)
	require.Equal(t, int64(2), repo.got.InviteeUserID)
	repo.err = errors.New("atomic reward failed")
	_, err = svc.recordFirstRecharge(ctx, input)
	require.ErrorIs(t, err, repo.err)
}
