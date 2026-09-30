package service

import (
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	SettingMerchantTestUserIDs          = "PAYMENT_MERCHANT_TEST_USER_IDS"
	SettingBalanceRetailPricingEnabled  = "BALANCE_RETAIL_PRICING_ENABLED"
	SettingBalanceRetailCostRate        = "BALANCE_RETAIL_COST_RATE"
	SettingBalanceRetailFixedCostGBP    = "BALANCE_RETAIL_FIXED_COST_GBP"
	SettingBalanceRetailQuoteTTLSeconds = "BALANCE_RETAIL_QUOTE_TTL_SECONDS"
	SettingBalanceRetailFXSource        = "BALANCE_RETAIL_FX_SOURCE"
	SettingBalanceRetailFXUSDPerGBP     = "BALANCE_RETAIL_FX_USD_PER_GBP"
	SettingBalanceRetailFXCNYPerGBP     = "BALANCE_RETAIL_FX_CNY_PER_GBP"
	SettingBalanceRetailFXAsOf          = "BALANCE_RETAIL_FX_ASOF"
	SettingBalanceRetailFXMaxAgeHours   = "BALANCE_RETAIL_FX_MAX_AGE_HOURS"
)

var retailPaymentSettingsKeys = []string{SettingMerchantTestUserIDs, SettingBalanceRetailPricingEnabled, SettingBalanceRetailCostRate, SettingBalanceRetailFixedCostGBP, SettingBalanceRetailQuoteTTLSeconds, SettingBalanceRetailFXSource, SettingBalanceRetailFXUSDPerGBP, SettingBalanceRetailFXCNYPerGBP, SettingBalanceRetailFXAsOf, SettingBalanceRetailFXMaxAgeHours}

func parseRetailPaymentConfig(cfg *PaymentConfig, vals map[string]string) {
	var merchantIDs []int64
	if json.Unmarshal([]byte(vals[SettingMerchantTestUserIDs]), &merchantIDs) == nil && len(merchantIDs) <= 50 {
		valid := true
		seen := map[int64]bool{}
		for _, id := range merchantIDs {
			if id <= 0 || seen[id] {
				valid = false
				break
			}
			seen[id] = true
		}
		if valid {
			cfg.MerchantTestUserIDs = merchantIDs
		}
	}
	cfg.BalanceRetailPricingEnabled = vals[SettingBalanceRetailPricingEnabled] == "true"
	cfg.BalanceRetailCostRate = pcParseFloat(vals[SettingBalanceRetailCostRate], 2)
	cfg.BalanceRetailFixedCostGBP = pcParseFloat(vals[SettingBalanceRetailFixedCostGBP], 0.25)
	cfg.BalanceRetailQuoteTTLSeconds = pcParseInt(vals[SettingBalanceRetailQuoteTTLSeconds], 900)
	cfg.BalanceRetailFXSource = strings.TrimSpace(vals[SettingBalanceRetailFXSource])
	if cfg.BalanceRetailFXSource == "" {
		cfg.BalanceRetailFXSource = "manual"
	}
	cfg.BalanceRetailFXUSDPerGBP = pcParseFloat(vals[SettingBalanceRetailFXUSDPerGBP], 0)
	cfg.BalanceRetailFXCNYPerGBP = pcParseFloat(vals[SettingBalanceRetailFXCNYPerGBP], 0)
	cfg.BalanceRetailFXAsOf = strings.TrimSpace(vals[SettingBalanceRetailFXAsOf])
	cfg.BalanceRetailFXMaxAgeHours = pcParseInt(vals[SettingBalanceRetailFXMaxAgeHours], 120)
}
func retailPaymentConfigUpdates(req UpdatePaymentConfigRequest, m map[string]string) error {
	bad := func(message string) error { return infraerrors.BadRequest("INVALID_RETAIL_PRICING_CONFIG", message) }
	if req.MerchantTestUserIDs != nil {
		if len(req.MerchantTestUserIDs) > 50 {
			return bad("too many merchant test users")
		}
		seen := map[int64]bool{}
		for _, id := range req.MerchantTestUserIDs {
			if id <= 0 || seen[id] {
				return bad("merchant test user IDs must be positive and unique")
			}
			seen[id] = true
		}
		encoded, _ := json.Marshal(req.MerchantTestUserIDs)
		m[SettingMerchantTestUserIDs] = string(encoded)
	}
	for _, field := range []struct {
		key       string
		v         *float64
		max       float64
		strict    bool
		precision bool
	}{
		{SettingBalanceRetailCostRate, req.BalanceRetailCostRate, 100, true, true},
		{SettingBalanceRetailFixedCostGBP, req.BalanceRetailFixedCostGBP, 100, false, true},
		{SettingBalanceRetailFXUSDPerGBP, req.BalanceRetailFXUSDPerGBP, 1e9, false, false},
		{SettingBalanceRetailFXCNYPerGBP, req.BalanceRetailFXCNYPerGBP, 1e9, false, false},
	} {
		if field.v == nil {
			continue
		}
		v := *field.v
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > field.max || (field.strict && v == field.max) {
			return bad(field.key + " is outside its supported range")
		}
		if strings.Contains(field.key, "FX_") && v == 0 {
			return bad(field.key + " must be positive")
		}
		if field.precision && math.Abs(v*100-math.Round(v*100)) > 1e-8 {
			return bad(field.key + " allows at most two decimal places")
		}
		m[field.key] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	if req.BalanceRetailPricingEnabled != nil {
		m[SettingBalanceRetailPricingEnabled] = strconv.FormatBool(*req.BalanceRetailPricingEnabled)
	}
	if req.BalanceRetailQuoteTTLSeconds != nil {
		v := *req.BalanceRetailQuoteTTLSeconds
		if v < 60 || v > 3600 {
			return bad("retail quote lifetime must be between 60 and 3600 seconds")
		}
		m[SettingBalanceRetailQuoteTTLSeconds] = strconv.Itoa(v)
	}
	if req.BalanceRetailFXMaxAgeHours != nil {
		v := *req.BalanceRetailFXMaxAgeHours
		if v < 24 || v > 240 {
			return bad("FX maximum age must be between 24 and 240 hours")
		}
		m[SettingBalanceRetailFXMaxAgeHours] = strconv.Itoa(v)
	}
	if req.BalanceRetailFXSource != nil {
		v := strings.TrimSpace(*req.BalanceRetailFXSource)
		if v != "manual" && v != "ecb" {
			return bad("retail FX source must be manual or ecb")
		}
		m[SettingBalanceRetailFXSource] = v
	}
	if req.BalanceRetailFXAsOf != nil {
		v := strings.TrimSpace(*req.BalanceRetailFXAsOf)
		if v != "" {
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				return bad("manual FX timestamp must use RFC3339")
			}
		}
		m[SettingBalanceRetailFXAsOf] = v
	}
	return nil
}
