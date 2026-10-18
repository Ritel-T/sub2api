package handler

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// checkoutPlansForResponse derives entitlements from the current linked group.
// Product identity, custom benefits, price and term remain owned by the plan.
func checkoutPlansForResponse(plans []*dbent.SubscriptionPlan, groups map[int64]service.PlanGroupInfo, language string) []checkoutPlan {
	result := make([]checkoutPlan, 0, len(plans))
	for _, plan := range plans {
		if plan == nil || !plan.ForSale {
			continue
		}
		info, exists := groups[plan.GroupID]
		if !exists || info.Status != service.StatusActive || info.SubscriptionType != service.SubscriptionTypeSubscription {
			continue
		}
		result = append(result, checkoutPlan{
			ID: plan.ID, GroupID: plan.GroupID, GroupPlatform: info.Platform, GroupName: info.I18n.LocalizeName(language, info.Name), GroupStatus: info.Status,
			RateMultiplier: info.RateMultiplier, PeakRateEnabled: info.PeakRateEnabled, PeakStart: info.PeakStart, PeakEnd: info.PeakEnd, PeakRateMultiplier: info.PeakRateMultiplier,
			DailyLimitUSD: info.DailyLimitUSD, WeeklyLimitUSD: info.WeeklyLimitUSD, MonthlyLimitUSD: info.MonthlyLimitUSD, ModelScopes: info.ModelScopes,
			Name: plan.Name, Description: plan.Description, Price: plan.Price, OriginalPrice: plan.OriginalPrice, Currency: plan.Currency,
			ValidityDays: plan.ValidityDays, ValidityUnit: plan.ValidityUnit, Features: parseFeatures(plan.Features), ProductName: plan.ProductName, ForSale: plan.ForSale, SortOrder: plan.SortOrder,
		})
	}
	return result
}

// Preserve the legacy /plans string feature wire format while sharing live terms
// and availability filtering with /checkout-info.
type publicSubscriptionPlan struct {
	checkoutPlan
	Features string `json:"features"`
}

func publicPlansForResponse(plans []*dbent.SubscriptionPlan, groups map[int64]service.PlanGroupInfo, language string) []publicSubscriptionPlan {
	rawFeatures := make(map[int64]string, len(plans))
	for _, plan := range plans {
		if plan != nil {
			rawFeatures[plan.ID] = plan.Features
		}
	}
	projected := checkoutPlansForResponse(plans, groups, language)
	result := make([]publicSubscriptionPlan, 0, len(projected))
	for _, plan := range projected {
		result = append(result, publicSubscriptionPlan{checkoutPlan: plan, Features: rawFeatures[plan.ID]})
	}
	return result
}
