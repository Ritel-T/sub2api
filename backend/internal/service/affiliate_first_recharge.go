package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// AffiliateFirstRecharge records the first actual positive balance recharge,
// including ineligible first recharges. Recording an ineligible event prevents
// a later recharge, configuration change or inviter binding from earning it.
type AffiliateFirstRecharge struct {
	InviteeUserID int64
	RedeemCodeID  int64
	SourceOrderID *int64
	Amount        float64
	Enabled       bool
	Bonus         float64
	MinimumAmount float64
}

type AffiliateFirstRechargeResult struct {
	InviterUserID int64
	Amount        float64
}

type AffiliateFirstRechargeRepository interface {
	RecordFirstRecharge(ctx context.Context, recharge AffiliateFirstRecharge) (*AffiliateFirstRechargeResult, error)
}

// Read the money policy strictly in the redemption transaction. A settings
// outage or malformed amount must roll back both the recharge and its reward,
// rather than silently consume the user's only opportunity to earn the gift.
func (s *AffiliateService) recordFirstRecharge(ctx context.Context, recharge AffiliateFirstRecharge) (*AffiliateFirstRechargeResult, error) {
	if dbent.TxFromContext(ctx) == nil {
		return nil, fmt.Errorf("first recharge requires redemption transaction")
	}
	if s.settingService == nil || s.settingService.settingRepo == nil {
		return nil, nil
	}
	values, err := s.settingService.settingRepo.GetMultiple(ctx, []string{
		SettingKeyAffiliateEnabled, SettingKeyAffiliateFirstRechargeBonus, SettingKeyAffiliateFirstRechargeMinAmount,
	})
	if err != nil {
		return nil, fmt.Errorf("read affiliate first recharge settings: %w", err)
	}
	if raw := values[SettingKeyAffiliateEnabled]; raw != "" && raw != "true" && raw != "false" {
		return nil, fmt.Errorf("invalid affiliate enabled configuration")
	}
	recharge.Enabled = values[SettingKeyAffiliateEnabled] == "true"
	if recharge.Bonus, err = strictFirstRechargeSetting(values[SettingKeyAffiliateFirstRechargeBonus], AffiliateFirstRechargeBonusMax); err != nil {
		return nil, fmt.Errorf("invalid affiliate first recharge bonus: %w", err)
	}
	if recharge.MinimumAmount, err = strictFirstRechargeSetting(values[SettingKeyAffiliateFirstRechargeMinAmount], AffiliateFirstRechargeMinAmountMax); err != nil {
		return nil, fmt.Errorf("invalid affiliate first recharge minimum: %w", err)
	}
	repo, ok := s.repo.(AffiliateFirstRechargeRepository)
	if !ok {
		if recharge.Enabled && recharge.Bonus > 0 {
			return nil, fmt.Errorf("atomic affiliate first recharge repository unavailable")
		}
		return nil, nil
	}
	return repo.RecordFirstRecharge(ctx, recharge)
}

func strictFirstRechargeSetting(raw string, maximum float64) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maximum {
		return 0, fmt.Errorf("amount must be finite and between 0 and %g", maximum)
	}
	return math.Round(value*1e8) / 1e8, nil
}

func (s *SettingService) GetAffiliateFirstRechargeBonus(ctx context.Context) float64 {
	return s.getFirstRechargeDisplayAmount(ctx, SettingKeyAffiliateFirstRechargeBonus, AffiliateFirstRechargeBonusMax)
}

func (s *SettingService) GetAffiliateFirstRechargeMinAmount(ctx context.Context) float64 {
	return s.getFirstRechargeDisplayAmount(ctx, SettingKeyAffiliateFirstRechargeMinAmount, AffiliateFirstRechargeMinAmountMax)
}

func (s *SettingService) getFirstRechargeDisplayAmount(ctx context.Context, key string, maximum float64) float64 {
	if s == nil || s.settingRepo == nil {
		return 0
	}
	raw, err := s.settingRepo.GetValue(ctx, key)
	if err != nil {
		return 0
	}
	value, err := strictFirstRechargeSetting(raw, maximum)
	if err != nil {
		return 0
	}
	return value
}
