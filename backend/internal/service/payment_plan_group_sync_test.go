package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestPaymentPlanLiveGroupTermsAndSaleAvailability(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentConfigService{entClient: client}
	source, err := client.Group.Create().SetName("Live group").SetPlatform(PlatformOpenAI).SetStatus(StatusActive).SetSubscriptionType(SubscriptionTypeSubscription).SetDailyLimitUsd(10).SetWeeklyLimitUsd(70).SetMonthlyLimitUsd(280).Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetName("Product identity").SetDescription("Custom benefit").SetGroupID(source.ID).SetPrice(19).SetValidityDays(30).SetValidityUnit("day").SetFeatures("Priority support").SetForSale(true).Save(ctx)
	require.NoError(t, err)
	list, err := svc.ListPlansForSale(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	info := svc.GetGroupInfoMap(ctx, list)[source.ID]
	require.Equal(t, "Live group", info.Name)
	require.EqualValues(t, 280, *info.MonthlyLimitUSD)
	_, err = client.Group.UpdateOneID(source.ID).SetName("Renamed group").SetMonthlyLimitUsd(1120).SetDailyLimitUsd(0).Save(ctx)
	require.NoError(t, err)
	// The plan itself is unchanged: repeated reads follow current group settings.
	info = svc.GetGroupInfoMap(ctx, list)[source.ID]
	require.Equal(t, "Renamed group", info.Name)
	require.EqualValues(t, 1120, *info.MonthlyLimitUSD)
	require.Zero(t, *info.DailyLimitUSD)
	require.Equal(t, StatusActive, info.Status)
	saved, err := svc.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "Product identity", saved.Name)
	require.EqualValues(t, 19, saved.Price)
	require.Equal(t, 30, saved.ValidityDays)
	require.Equal(t, "Priority support", saved.Features)
	for _, state := range []string{"off-sale", "inactive", "standard", "deleted"} {
		t.Run(state, func(t *testing.T) {
			require.NoError(t, client.SubscriptionPlan.UpdateOneID(plan.ID).SetForSale(state != "off-sale").Exec(ctx))
			update := client.Group.UpdateOneID(source.ID).SetStatus(StatusActive).SetSubscriptionType(SubscriptionTypeSubscription).ClearDeletedAt()
			if state == "inactive" {
				update.SetStatus(StatusDisabled)
			}
			if state == "standard" {
				update.SetSubscriptionType(SubscriptionTypeStandard)
			}
			if state == "deleted" {
				update.SetDeletedAt(time.Now())
			}
			require.NoError(t, update.Exec(ctx))
			plans, err := svc.ListPlansForSale(ctx)
			require.NoError(t, err)
			require.Empty(t, plans, "unavailable group/plan must not remain on the purchase page")
			all, err := svc.ListPlans(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1, "admin retains the product for repair")
			if state == "deleted" {
				require.Empty(t, svc.GetGroupInfoMap(ctx, []*dbent.SubscriptionPlan{nil, plan}))
			}
		})
	}
}
