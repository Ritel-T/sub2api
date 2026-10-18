package handler

import (
	"encoding/json"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPaymentPlanProjectionUsesGroupTermsAndKeepsProduct(t *testing.T) {
	monthly := 1120.0
	plan := &dbent.SubscriptionPlan{ID: 1, GroupID: 2, Name: "Product", Description: "Custom promise", Price: 19, ValidityDays: 30, ValidityUnit: "day", Features: "Priority support", ForSale: true}
	groups := map[int64]service.PlanGroupInfo{2: {Name: "Current group", Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription, Platform: service.PlatformOpenAI, MonthlyLimitUSD: &monthly}}
	checkout := checkoutPlansForResponse([]*dbent.SubscriptionPlan{plan}, groups, "en")
	require.Len(t, checkout, 1)
	require.Equal(t, "Current group", checkout[0].GroupName)
	require.Equal(t, &monthly, checkout[0].MonthlyLimitUSD)
	require.Equal(t, plan.Name, checkout[0].Name)
	require.Equal(t, plan.Description, checkout[0].Description)
	require.Equal(t, plan.Price, checkout[0].Price)
	require.Equal(t, plan.ValidityDays, checkout[0].ValidityDays)
	require.Equal(t, []string{"Priority support"}, checkout[0].Features)
	public := publicPlansForResponse([]*dbent.SubscriptionPlan{plan}, groups, "en")
	payload, err := json.Marshal(public)
	require.NoError(t, err)
	var wire []map[string]any
	require.NoError(t, json.Unmarshal(payload, &wire))
	require.Equal(t, "Priority support", wire[0]["features"], "legacy plans feature wire remains a string")
	require.EqualValues(t, monthly, wire[0]["monthly_limit_usd"])
	require.Equal(t, "active", wire[0]["group_status"])
}

func TestPaymentPlanProjectionFailsClosedForUnavailableTerms(t *testing.T) {
	for _, state := range []string{"missing", "inactive", "standard", "off-sale"} {
		t.Run(state, func(t *testing.T) {
			plan := &dbent.SubscriptionPlan{ID: 1, GroupID: 2, ForSale: state != "off-sale"}
			info := service.PlanGroupInfo{Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription}
			if state == "inactive" {
				info.Status = service.StatusDisabled
			}
			if state == "standard" {
				info.SubscriptionType = service.SubscriptionTypeStandard
			}
			groups := map[int64]service.PlanGroupInfo{2: info}
			if state == "missing" {
				groups = nil
			}
			require.Empty(t, checkoutPlansForResponse([]*dbent.SubscriptionPlan{nil, plan}, groups, "en"))
			require.Empty(t, publicPlansForResponse([]*dbent.SubscriptionPlan{plan}, groups, "en"))
		})
	}
}
