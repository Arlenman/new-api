package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type subscriptionPlansResponse struct {
	Success bool                  `json:"success"`
	Data    []SubscriptionPlanDTO `json:"data"`
}

func TestGetSubscriptionPlansFiltersByCurrentDatabaseUserGroup(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	confirmPaymentComplianceForTest(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))

	user := &model.User{Id: 9701, Username: "subscription-group-user", Password: "password123", AffCode: "subscription-group-aff", Group: "B"}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create([]model.SubscriptionPlan{
		{Id: 9702, Title: "All Groups", Enabled: true, UserGroup: ""},
		{Id: 9703, Title: "Group B", Enabled: true, UserGroup: "B"},
		{Id: 9704, Title: "Group A", Enabled: true, UserGroup: "A"},
		{Id: 9705, Title: "Disabled Group B", Enabled: true, UserGroup: "B"},
	}).Error)
	require.NoError(t, db.Model(&model.SubscriptionPlan{}).Where("id = ?", 9705).Update("enabled", false).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", user.Id)
	ctx.Set("group", "A") // Session may be stale and must not drive filtering.

	GetSubscriptionPlans(ctx)

	var response subscriptionPlansResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 2)
	require.Equal(t, "Group B", response.Data[0].Plan.Title)
	require.Equal(t, "All Groups", response.Data[1].Plan.Title)
}

func TestNormalizeSubscriptionPlanUserGroup(t *testing.T) {
	tests := []struct {
		name      string
		userGroup string
		wantGroup string
		wantError string
	}{
		{name: "all groups", userGroup: "   ", wantGroup: ""},
		{name: "known group", userGroup: " vip ", wantGroup: "vip"},
		{name: "unknown group", userGroup: "missing-group", wantGroup: "missing-group", wantError: "适用分组不存在"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := model.SubscriptionPlan{UserGroup: tt.userGroup}
			err := normalizeSubscriptionPlanUserGroup(&plan)
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantGroup, plan.UserGroup)
		})
	}
}

type apiErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func TestSubscriptionPaymentHandlersRejectDifferentUserGroupBeforeCreatingOrder(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		handler gin.HandlerFunc
	}{
		{name: "epay", body: `{"plan_id":9802,"payment_method":"alipay"}`, handler: SubscriptionRequestEpay},
		{name: "stripe", body: `{"plan_id":9802}`, handler: SubscriptionRequestStripePay},
		{name: "creem", body: `{"plan_id":9802}`, handler: SubscriptionRequestCreemPay},
		{name: "waffo pancake", body: `{"plan_id":9802}`, handler: SubscriptionRequestWaffoPancakePay},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			confirmPaymentComplianceForTest(t)
			require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionOrder{}))

			user := &model.User{Id: 9801, Username: "payment-group-user", Password: "password123", AffCode: "payment-group-aff", Group: "B"}
			plan := &model.SubscriptionPlan{Id: 9802, Title: "Group A Plan", Enabled: true, UserGroup: "A"}
			require.NoError(t, db.Create(user).Error)
			require.NoError(t, db.Create(plan).Error)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("id", user.Id)
			ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			tt.handler(ctx)

			var response apiErrorResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.False(t, response.Success)
			require.Equal(t, "该套餐不适用于当前用户分组", response.Message)

			var orderCount int64
			require.NoError(t, db.Model(&model.SubscriptionOrder{}).Count(&orderCount).Error)
			require.Zero(t, orderCount)
		})
	}
}

func TestAdminCreateSubscriptionPlanValidatesUserGroup(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	confirmPaymentComplianceForTest(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))

	tests := []struct {
		name        string
		body        string
		wantSuccess bool
		wantMessage string
		wantGroup   string
	}{
		{
			name:        "rejects unknown group",
			body:        `{"plan":{"title":"Unknown Group Plan","price_amount":0,"duration_unit":"month","duration_value":1,"enabled":true,"user_group":"missing-group"}}`,
			wantSuccess: false,
			wantMessage: "适用分组不存在",
		},
		{
			name:        "allows all groups",
			body:        `{"plan":{"title":"All Groups Plan","price_amount":0,"duration_unit":"month","duration_value":1,"enabled":true,"user_group":"   "}}`,
			wantSuccess: true,
			wantGroup:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			AdminCreateSubscriptionPlan(ctx)

			var response apiErrorResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, tt.wantSuccess, response.Success)
			if tt.wantMessage != "" {
				require.Equal(t, tt.wantMessage, response.Message)
			}
			if tt.wantSuccess {
				var plan model.SubscriptionPlan
				require.NoError(t, db.Where("title = ?", "All Groups Plan").First(&plan).Error)
				require.Equal(t, tt.wantGroup, plan.UserGroup)
			}
		})
	}

	var invalidCount int64
	require.NoError(t, db.Model(&model.SubscriptionPlan{}).Where("title = ?", "Unknown Group Plan").Count(&invalidCount).Error)
	require.Zero(t, invalidCount)
}

func TestAdminUpdateSubscriptionPlanValidatesUserGroup(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	confirmPaymentComplianceForTest(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))

	plan := &model.SubscriptionPlan{
		Id:            9851,
		Title:         "Existing Plan",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		UserGroup:     "vip",
	}
	require.NoError(t, db.Create(plan).Error)

	tests := []struct {
		name        string
		body        string
		wantSuccess bool
		wantMessage string
		wantGroup   string
	}{
		{
			name:        "rejects unknown group",
			body:        `{"plan":{"title":"Existing Plan","price_amount":0,"duration_unit":"month","duration_value":1,"enabled":true,"user_group":"missing-group"}}`,
			wantSuccess: false,
			wantMessage: "适用分组不存在",
			wantGroup:   "vip",
		},
		{
			name:        "allows all groups",
			body:        `{"plan":{"title":"Existing Plan","price_amount":0,"duration_unit":"month","duration_value":1,"enabled":true,"user_group":"   "}}`,
			wantSuccess: true,
			wantGroup:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "id", Value: "9851"}}
			ctx.Request = httptest.NewRequest("PUT", "/", strings.NewReader(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			AdminUpdateSubscriptionPlan(ctx)

			var response apiErrorResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, tt.wantSuccess, response.Success)
			if tt.wantMessage != "" {
				require.Equal(t, tt.wantMessage, response.Message)
			}

			var updated model.SubscriptionPlan
			require.NoError(t, db.First(&updated, plan.Id).Error)
			require.Equal(t, tt.wantGroup, updated.UserGroup)
		})
	}
}

func TestAdminListSubscriptionPlansDoesNotFilterByUserGroup(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}))
	require.NoError(t, db.Create([]model.SubscriptionPlan{
		{Id: 9901, Title: "All Groups", Enabled: true, UserGroup: ""},
		{Id: 9902, Title: "Group A", Enabled: true, UserGroup: "A"},
		{Id: 9903, Title: "Disabled Group B", Enabled: false, UserGroup: "B"},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	AdminListSubscriptionPlans(ctx)

	var response subscriptionPlansResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data, 3)
}
