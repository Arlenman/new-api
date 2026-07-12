package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedUserSubscriptionForUpdateTest(t *testing.T, sub *UserSubscription) int {
	t.Helper()
	require.NoError(t, DB.Create(&User{Id: sub.UserId, Username: "subscription-user", Password: "password123", AffCode: "sub-aff"}).Error)
	require.NoError(t, DB.Create(&SubscriptionPlan{
		Id:            sub.PlanId,
		Title:         "Editable Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}).Error)
	require.NoError(t, DB.Create(sub).Error)
	return sub.Id
}

func TestAdminUpdateUserSubscriptionReactivatesExpiredSubscription(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	subID := seedUserSubscriptionForUpdateTest(t, &UserSubscription{
		UserId:      10,
		PlanId:      20,
		AmountTotal: 100,
		AmountUsed:  80,
		StartTime:   now - 7200,
		EndTime:     now - 3600,
		Status:      "expired",
		Source:      "admin",
	})

	_, err := AdminUpdateUserSubscription(subID, now+3600, "add", 50)
	require.NoError(t, err)

	var updated UserSubscription
	require.NoError(t, DB.First(&updated, subID).Error)
	require.Equal(t, "active", updated.Status)
	require.EqualValues(t, now+3600, updated.EndTime)
	require.EqualValues(t, 150, updated.AmountTotal)
	require.EqualValues(t, 80, updated.AmountUsed)
}

func TestAdminUpdateUserSubscriptionAllowsQuotaBelowUsed(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	subID := seedUserSubscriptionForUpdateTest(t, &UserSubscription{
		UserId:      11,
		PlanId:      21,
		AmountTotal: 100,
		AmountUsed:  80,
		StartTime:   now - 7200,
		EndTime:     now + 7200,
		Status:      "active",
		Source:      "admin",
	})

	_, err := AdminUpdateUserSubscription(subID, now+7200, "override", 10)
	require.NoError(t, err)

	var updated UserSubscription
	require.NoError(t, DB.First(&updated, subID).Error)
	require.Equal(t, "active", updated.Status)
	require.EqualValues(t, 10, updated.AmountTotal)
	require.EqualValues(t, 80, updated.AmountUsed)

	_, err = AdminUpdateUserSubscription(subID, now+7200, "subtract", 5)
	require.NoError(t, err)
	require.NoError(t, DB.First(&updated, subID).Error)
	require.EqualValues(t, 5, updated.AmountTotal)
	require.EqualValues(t, 80, updated.AmountUsed)
}
