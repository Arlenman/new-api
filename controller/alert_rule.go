package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAlertRules(c *gin.Context) {
	views, err := service.ListAlertRuleViews()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, views)
}

func CreateAlertRule(c *gin.Context) {
	var input dto.AlertRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorMsg(c, "invalid alert rule request")
		return
	}
	validated, err := service.ValidateAlertRuleInput(c.Request.Context(), input, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule, err := service.NewAlertRuleFromInput(validated)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = model.CreateAlertRule(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	if rule.Enabled && rule.TriggerType == model.AlertRuleTriggerTypeEnabledChannelCount {
		if evaluateErr := service.EvaluateEnabledChannelCountAlertRules(c.Request.Context()); evaluateErr != nil {
			common.SysError("evaluate enabled channel count alert after rule creation: " + evaluateErr.Error())
		}
	}
	view, err := service.AlertRuleToView(rule, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func UpdateAlertRule(c *gin.Context) {
	id, ok := parseAlertRuleID(c)
	if !ok {
		return
	}
	var input dto.AlertRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorMsg(c, "invalid alert rule request")
		return
	}
	validated, err := service.ValidateAlertRuleInput(c.Request.Context(), input, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule, err := service.NewAlertRuleFromInput(validated)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	rule.ID = id
	if err = model.UpdateAlertRule(rule); err != nil {
		writeAlertRuleModelError(c, err)
		return
	}
	updated, err := model.GetAlertRuleByID(id)
	if err != nil {
		writeAlertRuleModelError(c, err)
		return
	}
	if updated.Enabled && updated.TriggerType == model.AlertRuleTriggerTypeEnabledChannelCount {
		if evaluateErr := service.EvaluateEnabledChannelCountAlertRules(c.Request.Context()); evaluateErr != nil {
			common.SysError("evaluate enabled channel count alert after rule update: " + evaluateErr.Error())
		}
	}
	view, err := service.AlertRuleToView(updated, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func DeleteAlertRule(c *gin.Context) {
	id, ok := parseAlertRuleID(c)
	if !ok {
		return
	}
	if err := model.DeleteAlertRule(id); err != nil {
		writeAlertRuleModelError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetAlertRuleProviders(c *gin.Context) {
	client, err := service.NewApiNoticeClientFromEnv()
	if err != nil {
		common.ApiErrorMsg(c, "api-notice configuration is invalid")
		return
	}
	providers, err := client.GetProviders(c.Request.Context())
	if err != nil {
		common.ApiErrorMsg(c, "api-notice provider catalog is unavailable")
		return
	}
	common.ApiSuccess(c, gin.H{
		"providers":          providers,
		"api_key_configured": service.ApiNoticeAPIKeyConfigured(),
	})
}

func GetApiNoticeConfig(c *gin.Context) {
	config, err := service.GetApiNoticeConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

func UpdateApiNoticeConfig(c *gin.Context) {
	var input dto.ApiNoticeConfigUpdate
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorMsg(c, "invalid api-notice configuration request")
		return
	}
	config, err := service.UpdateApiNoticeConfig(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

func PreviewAlertRule(c *gin.Context) {
	var request dto.AlertRulePreviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid alert rule preview request")
		return
	}
	validated, err := service.ValidateAlertRuleInput(c.Request.Context(), request.Rule, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := service.GetOptionalUpstreamChannel(request.ChannelID)
	if err != nil {
		writeAlertRuleModelError(c, err)
		return
	}
	message, err := service.BuildAlertPreview(validated, request.EventType, channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, message)
}

func TestAlertRuleConnection(c *gin.Context) {
	status, err := service.TestApiNoticeConnection(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
			"data":    status,
		})
		return
	}
	common.ApiSuccess(c, status)
}

func TestSendAlertRule(c *gin.Context) {
	var request dto.AlertRuleTestSendRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "invalid alert rule test request")
		return
	}
	validated, err := service.ValidateAlertRuleInput(c.Request.Context(), request.Rule, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := service.GetOptionalUpstreamChannel(request.ChannelID)
	if err != nil {
		writeAlertRuleModelError(c, err)
		return
	}
	message, err := service.BuildAlertPreview(validated, request.EventType, channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.SendAlertRuleTestNotice(c.Request.Context(), request.Providers, message)
	if err != nil {
		message := result.Error
		if message == "" {
			message = err.Error()
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": message,
			"data":    result,
		})
		return
	}
	common.ApiSuccess(c, result)
}

func parseAlertRuleID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid alert rule id")
		return 0, false
	}
	return id, true
}

func writeAlertRuleModelError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "alert rule not found"})
		return
	}
	common.ApiError(c, err)
}
