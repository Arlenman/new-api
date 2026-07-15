package model

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSubscriptionGroupFlowTestDB(t *testing.T) {
	t.Helper()

	originalDB := DB
	originalLogDB := LOG_DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "subscription-group.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&User{},
		&Log{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
	))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(2)

	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		require.NoError(t, sqlDB.Close())
	})
}

func TestSubscriptionPlanExposesUserGroup(t *testing.T) {
	field, ok := reflect.TypeOf(SubscriptionPlan{}).FieldByName("UserGroup")
	require.True(t, ok)
	require.Equal(t, "user_group", field.Tag.Get("json"))
	require.Contains(t, field.Tag.Get("gorm"), "type:varchar(64)")
}

func TestSubscriptionPlanIsAvailableForGroup(t *testing.T) {
	tests := []struct {
		name      string
		planGroup string
		userGroup string
		want      bool
	}{
		{name: "all groups plan", planGroup: "", userGroup: "B", want: true},
		{name: "same group", planGroup: "B", userGroup: "B", want: true},
		{name: "different group", planGroup: "A", userGroup: "B", want: false},
		{name: "trim configured group", planGroup: " B ", userGroup: "B", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := SubscriptionPlan{UserGroup: tt.planGroup}
			require.Equal(t, tt.want, plan.IsAvailableForGroup(tt.userGroup))
		})
	}
}

func TestSubscriptionPlanGroupMatchingIgnoresSpecialUsableGroups(t *testing.T) {
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	original := specialGroups.ReadAll()
	specialGroups.Clear()
	specialGroups.Set("B", map[string]string{"+:A": "cross-group access"})
	t.Cleanup(func() {
		specialGroups.Clear()
		specialGroups.AddAll(original)
	})

	plan := SubscriptionPlan{UserGroup: "A"}
	require.False(t, plan.IsAvailableForGroup("B"))
	require.True(t, plan.IsAvailableForGroup("A"))
}

func TestPurchaseSubscriptionWithBalanceRejectsDifferentUserGroupBeforeCharging(t *testing.T) {
	truncateTables(t)

	user := &User{Id: 9601, Username: "group-b-user", Password: "password123", AffCode: "group-b-aff", Group: "B", Quota: 100000000}
	plan := &SubscriptionPlan{
		Id:              9602,
		Title:           "Group A Plan",
		PriceAmount:     1,
		Currency:        "USD",
		DurationUnit:    SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		AllowBalancePay: common.GetPointer(true),
		UserGroup:       "A",
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(plan).Error)

	err := PurchaseSubscriptionWithBalance(user.Id, plan.Id)
	require.EqualError(t, err, "该套餐不适用于当前用户分组")

	var updated User
	require.NoError(t, DB.First(&updated, user.Id).Error)
	require.Equal(t, user.Quota, updated.Quota)

	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.Zero(t, subscriptionCount)
}

func TestPurchaseSubscriptionWithBalanceAllowsAllGroupsPlan(t *testing.T) {
	setupSubscriptionGroupFlowTestDB(t)

	user := &User{Id: 9611, Username: "all-group-user", Password: "password123", AffCode: "all-group-aff", Group: "B", Quota: 100000000}
	plan := &SubscriptionPlan{
		Id:              9612,
		Title:           "Historical All Groups Plan",
		PriceAmount:     0,
		Currency:        "USD",
		DurationUnit:    SubscriptionDurationMonth,
		DurationValue:   1,
		Enabled:         true,
		AllowBalancePay: common.GetPointer(true),
		UserGroup:       "",
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(plan).Error)

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&subscriptionCount).Error)
	require.EqualValues(t, 1, subscriptionCount)

	var orderCount int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&orderCount).Error)
	require.EqualValues(t, 1, orderCount)
}

func TestAdminBindSubscriptionAllowsDifferentUserGroup(t *testing.T) {
	setupSubscriptionGroupFlowTestDB(t)

	user := &User{Id: 9621, Username: "admin-bind-group-user", Password: "password123", AffCode: "admin-bind-group-aff", Group: "B"}
	plan := &SubscriptionPlan{
		Id:            9622,
		Title:         "Admin Group A Plan",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		UserGroup:     "A",
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(plan).Error)

	_, err := AdminBindSubscription(user.Id, plan.Id, "")
	require.NoError(t, err)

	var subscriptionCount int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&subscriptionCount).Error)
	require.EqualValues(t, 1, subscriptionCount)
}
