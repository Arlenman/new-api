package controller

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type upstreamChannelView struct {
	ID                       int                       `json:"id"`
	Name                     string                    `json:"name"`
	BaseURL                  string                    `json:"base_url"`
	Provider                 string                    `json:"provider"`
	AuthType                 string                    `json:"auth_type"`
	Priority                 int64                     `json:"priority"`
	SelectedGroup            string                    `json:"selected_group"`
	Username                 string                    `json:"username"`
	Note                     string                    `json:"note"`
	DefaultTestModel         string                    `json:"default_test_model"`
	HasPassword              bool                      `json:"has_password"`
	SourceChannelCount       int                       `json:"source_channel_count"`
	ActiveSourceChannelCount int                       `json:"active_source_channel_count"`
	InUseKeyCount            int                       `json:"in_use_key_count"`
	Balance                  float64                   `json:"balance"`
	Availability24h          *float64                  `json:"availability_24h"`
	AverageFirstTokenLatency *float64                  `json:"average_first_token_latency_ms"`
	BalanceUpdatedTime       int64                     `json:"balance_updated_time"`
	BalanceThreshold         float64                   `json:"balance_threshold"`
	Multiplier               float64                   `json:"multiplier"`
	AutoRefreshInterval      int                       `json:"auto_refresh_interval"`
	LastSyncTime             int64                     `json:"last_sync_time"`
	LastError                string                    `json:"last_error"`
	LastErrorCode            string                    `json:"last_error_code,omitempty"`
	Status                   string                    `json:"status"`
	Snapshot                 *service.UpstreamSnapshot `json:"snapshot,omitempty"`
}

const (
	upstreamChannelNameMaxLength   = 255
	upstreamChannelNoteMaxLength   = 2000
	upstreamSelectedGroupMaxLength = 255
)

type createUpstreamChannelRequest struct {
	BaseURL             string   `json:"base_url"`
	Name                string   `json:"name"`
	Provider            string   `json:"provider"`
	AuthType            string   `json:"auth_type"`
	Priority            *int64   `json:"priority"`
	Username            string   `json:"username"`
	Password            string   `json:"password"`
	BalanceThreshold    float64  `json:"balance_threshold"`
	Multiplier          *float64 `json:"multiplier"`
	AutoRefreshInterval int      `json:"auto_refresh_interval"`
}

type updateUpstreamChannelRequest struct {
	Name                *string  `json:"name"`
	Provider            string   `json:"provider"`
	AuthType            *string  `json:"auth_type"`
	Priority            *int64   `json:"priority"`
	Username            string   `json:"username"`
	Password            string   `json:"password"`
	BalanceThreshold    float64  `json:"balance_threshold"`
	Multiplier          *float64 `json:"multiplier"`
	AutoRefreshInterval int      `json:"auto_refresh_interval"`
}

type updateUpstreamChannelNoteRequest struct {
	Note string `json:"note"`
}

type updateUpstreamChannelSelectedGroupRequest struct {
	SelectedGroup string `json:"selected_group"`
}

type updateUpstreamChannelDefaultTestModelRequest struct {
	DefaultTestModel string `json:"default_test_model"`
}

type updateUpstreamPriorityScheduleRequest struct {
	Enabled               bool `json:"enabled"`
	IntervalSeconds       int  `json:"interval_seconds"`
	MaxTestLatencySeconds int  `json:"max_test_latency_seconds"`
}

type importUpstreamChannelKeysRequest struct {
	KeyIDs     []int64   `json:"key_ids"`
	Groups     *[]string `json:"groups"`
	Tag        *string   `json:"tag"`
	NamePrefix *string   `json:"name_prefix"`
	Priority   *int64    `json:"priority"`
	Weight     *int64    `json:"weight"`
	TestModel  *string   `json:"test_model"`
	Models     *[]string `json:"models"`
	AutoBan    *int      `json:"auto_ban"`
	Remark     *string   `json:"remark"`
}

type fetchUpstreamChannelKeyModelsRequest struct {
	KeyIDs []int64 `json:"key_ids"`
}

type updateUpstreamChannelKeyGroupRequest struct {
	Group   string `json:"group"`
	GroupID *int64 `json:"group_id"`
}

func CreateUpstreamChannel(c *gin.Context) {
	var request createUpstreamChannelRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	baseURL, err := service.NormalizeUpstreamBaseURL(request.BaseURL)
	if err != nil {
		common.ApiError(c, errInvalidUpstreamBaseURL)
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = model.UpstreamChannelDefaultName(baseURL)
	}
	if utf8.RuneCountInString(name) > upstreamChannelNameMaxLength {
		common.ApiError(c, errInvalidUpstreamChannelName)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(request.Provider))
	if provider == "" {
		provider = service.UpstreamProviderAuto
	}
	if provider != service.UpstreamProviderAuto && provider != service.UpstreamProviderNewAPI && provider != service.UpstreamProviderSub2API && provider != service.UpstreamProviderOther {
		common.ApiError(c, errInvalidUpstreamProvider)
		return
	}
	authType := model.NormalizeUpstreamAuthType(request.AuthType)
	priority := int64(0)
	if request.Priority != nil {
		priority = *request.Priority
	}
	if priority < math.MinInt32 || priority > math.MaxInt32 {
		common.ApiError(c, errInvalidUpstreamPriority)
		return
	}
	username := strings.TrimSpace(request.Username)
	if len(username) > 255 || len(request.Password) > 2048 {
		common.ApiError(c, errInvalidUpstreamCredential)
		return
	}
	if authErr := validateUpstreamAuthentication(provider, authType, username, strings.TrimSpace(request.Password) != ""); authErr != nil {
		common.ApiError(c, authErr)
		return
	}
	if math.IsNaN(request.BalanceThreshold) || math.IsInf(request.BalanceThreshold, 0) || request.BalanceThreshold < 0 || request.BalanceThreshold > 1_000_000_000 {
		common.ApiError(c, errInvalidUpstreamThreshold)
		return
	}
	multiplier := float64(model.UpstreamChannelDefaultMultiplier)
	if request.Multiplier != nil {
		multiplier = *request.Multiplier
	}
	if !validUpstreamChannelMultiplier(multiplier) {
		common.ApiError(c, errInvalidUpstreamMultiplier)
		return
	}
	if request.AutoRefreshInterval != 0 && (request.AutoRefreshInterval < 60 || request.AutoRefreshInterval > 86400) {
		common.ApiError(c, errInvalidUpstreamRefreshInterval)
		return
	}
	passwordCiphertext := ""
	if request.Password != "" {
		if !common.HasPersistentCryptoSecret() {
			common.ApiError(c, errUpstreamCryptoSecretRequired)
			return
		}
		passwordCiphertext, err = common.EncryptSecret("upstream-channel-password", request.Password)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	row, err := model.CreateUpstreamChannelConfig(model.UpstreamChannel{
		Name:                name,
		BaseURL:             baseURL,
		Provider:            provider,
		AuthType:            authType,
		Priority:            priority,
		Username:            username,
		PasswordCiphertext:  passwordCiphertext,
		BalanceThreshold:    request.BalanceThreshold,
		Multiplier:          multiplier,
		AutoRefreshInterval: request.AutoRefreshInterval,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func GetUpstreamChannels(c *gin.Context) {
	rows, stats, err := service.DiscoverUpstreamChannels()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logMetrics, metricsErr := service.GetUpstreamChannelLogMetricsSince(common.GetTimestamp() - 24*60*60)
	if metricsErr != nil {
		common.SysError("failed to load upstream channel log metrics: " + metricsErr.Error())
		logMetrics = map[string]service.UpstreamChannelLogMetrics{}
	}
	views := make([]upstreamChannelView, 0, len(rows))
	for _, row := range rows {
		normalizedBaseURL, normalizeErr := service.NormalizeUpstreamBaseURL(row.BaseURL)
		if normalizeErr != nil {
			normalizedBaseURL = row.BaseURL
		}
		channelStats := stats[normalizedBaseURL]
		view := buildUpstreamChannelView(row, channelStats.Total, channelStats.Active, channelStats.InUseKeyCount)
		metrics := logMetrics[row.BaseURL]
		view.Availability24h = metrics.Availability24h
		view.AverageFirstTokenLatency = metrics.AverageFirstTokenLatencyMs
		views = append(views, view)
	}
	common.ApiSuccess(c, views)
}

func DeleteUpstreamChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	if err = service.DeleteUpstreamChannel(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func UpdateUpstreamChannelConfig(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	row, err := model.GetUpstreamChannelByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var request updateUpstreamChannelRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	name := row.Name
	if request.Name != nil {
		name = strings.TrimSpace(*request.Name)
		if name == "" {
			name = model.UpstreamChannelDefaultName(row.BaseURL)
		}
	}
	if utf8.RuneCountInString(name) > upstreamChannelNameMaxLength {
		common.ApiError(c, errInvalidUpstreamChannelName)
		return
	}
	request.Provider = strings.ToLower(strings.TrimSpace(request.Provider))
	if request.Provider == "" {
		request.Provider = service.UpstreamProviderAuto
	}
	if request.Provider != service.UpstreamProviderAuto && request.Provider != service.UpstreamProviderNewAPI && request.Provider != service.UpstreamProviderSub2API && request.Provider != service.UpstreamProviderOther {
		common.ApiError(c, errInvalidUpstreamProvider)
		return
	}
	authType := row.EffectiveAuthType()
	if request.AuthType != nil {
		authType = model.NormalizeUpstreamAuthType(*request.AuthType)
	}
	priority := row.Priority
	if request.Priority != nil {
		priority = *request.Priority
	}
	if priority < math.MinInt32 || priority > math.MaxInt32 {
		common.ApiError(c, errInvalidUpstreamPriority)
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	if len(request.Username) > 255 || len(request.Password) > 2048 {
		common.ApiError(c, errInvalidUpstreamCredential)
		return
	}
	authTypeChanged := authType != row.EffectiveAuthType()
	hasCredential := strings.TrimSpace(request.Password) != "" || (row.HasPassword() && !authTypeChanged)
	if authErr := validateUpstreamAuthentication(request.Provider, authType, request.Username, hasCredential); authErr != nil {
		common.ApiError(c, authErr)
		return
	}
	if math.IsNaN(request.BalanceThreshold) || math.IsInf(request.BalanceThreshold, 0) || request.BalanceThreshold < 0 || request.BalanceThreshold > 1_000_000_000 {
		common.ApiError(c, errInvalidUpstreamThreshold)
		return
	}
	multiplier := row.EffectiveMultiplier()
	if request.Multiplier != nil {
		multiplier = *request.Multiplier
	}
	if !validUpstreamChannelMultiplier(multiplier) {
		common.ApiError(c, errInvalidUpstreamMultiplier)
		return
	}
	if request.AutoRefreshInterval != 0 && (request.AutoRefreshInterval < 60 || request.AutoRefreshInterval > 86400) {
		common.ApiError(c, errInvalidUpstreamRefreshInterval)
		return
	}

	var passwordCiphertext *string
	if request.Password != "" {
		if !common.HasPersistentCryptoSecret() {
			common.ApiError(c, errUpstreamCryptoSecretRequired)
			return
		}
		encrypted, encryptErr := common.EncryptSecret("upstream-channel-password", request.Password)
		if encryptErr != nil {
			common.ApiError(c, encryptErr)
			return
		}
		passwordCiphertext = &encrypted
	}
	if err = model.UpdateUpstreamChannelConfig(id, name, request.Provider, authType, request.Username, passwordCiphertext, request.BalanceThreshold, multiplier, request.AutoRefreshInterval, priority); err != nil {
		common.ApiError(c, err)
		return
	}
	row, err = model.GetUpstreamChannelByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func UpdateUpstreamChannelSelectedGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	var request updateUpstreamChannelSelectedGroupRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	request.SelectedGroup = strings.TrimSpace(request.SelectedGroup)
	if utf8.RuneCountInString(request.SelectedGroup) > upstreamSelectedGroupMaxLength {
		common.ApiError(c, errInvalidUpstreamSelectedGroup)
		return
	}
	row, err := service.UpdateUpstreamChannelSelectedGroup(id, request.SelectedGroup)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func UpdateUpstreamChannelDefaultTestModel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	var request updateUpstreamChannelDefaultTestModelRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	request.DefaultTestModel = strings.TrimSpace(request.DefaultTestModel)
	if utf8.RuneCountInString(request.DefaultTestModel) > upstreamChannelNameMaxLength {
		common.ApiError(c, errInvalidUpstreamDefaultTestModel)
		return
	}
	row, err := service.UpdateUpstreamChannelDefaultTestModel(id, request.DefaultTestModel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func GetUpstreamPrioritySchedule(c *gin.Context) {
	common.ApiSuccess(c, operation_setting.GetUpstreamPrioritySetting())
}

func UpdateUpstreamPrioritySchedule(c *gin.Context) {
	var request updateUpstreamPriorityScheduleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.IntervalSeconds < operation_setting.UpstreamPriorityMinIntervalSeconds || request.IntervalSeconds > operation_setting.UpstreamPriorityMaxIntervalSeconds {
		common.ApiError(c, errInvalidUpstreamPriorityInterval)
		return
	}
	if request.MaxTestLatencySeconds < operation_setting.UpstreamPriorityMinLatencySeconds || request.MaxTestLatencySeconds > operation_setting.UpstreamPriorityMaxLatencySeconds {
		common.ApiError(c, errInvalidUpstreamPriorityLatency)
		return
	}
	wasEnabled := operation_setting.GetUpstreamPrioritySetting().Enabled
	if err := model.UpdateOptionsBulk(map[string]string{
		"upstream_priority_setting.enabled":                  strconv.FormatBool(request.Enabled),
		"upstream_priority_setting.interval_seconds":         strconv.Itoa(request.IntervalSeconds),
		"upstream_priority_setting.max_test_latency_seconds": strconv.Itoa(request.MaxTestLatencySeconds),
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Enabled && !wasEnabled {
		if _, _, err := service.EnqueueSystemTask(model.SystemTaskTypeUpstreamPrioritySync, nil); err != nil {
			logger.LogWarn(c, fmt.Sprintf("enqueue upstream priority sync after enabling schedule failed: %v", err))
		}
	}
	common.ApiSuccess(c, operation_setting.GetUpstreamPrioritySetting())
}

func PinUpstreamChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	row, err := model.PinUpstreamChannel(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func UpdateUpstreamChannelNote(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	if _, err = model.GetUpstreamChannelByID(id); err != nil {
		common.ApiError(c, err)
		return
	}
	var request updateUpstreamChannelNoteRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	request.Note = strings.TrimSpace(request.Note)
	if utf8.RuneCountInString(request.Note) > upstreamChannelNoteMaxLength {
		common.ApiError(c, errInvalidUpstreamNote)
		return
	}
	if err = model.UpdateUpstreamChannelNote(id, request.Note); err != nil {
		common.ApiError(c, err)
		return
	}
	row, err := model.GetUpstreamChannelByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func RefreshUpstreamChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	row, _, err := service.RefreshUpstreamChannel(c.Request.Context(), id)
	if err != nil {
		var data any
		if row != nil {
			data = buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row))
		}
		respondUpstreamChannelError(c, err, data)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func RefreshUpstreamChannelBalance(c *gin.Context) {
	refreshUpstreamChannel(c, service.RefreshUpstreamChannelBalance)
}

func RefreshUpstreamChannelKeys(c *gin.Context) {
	refreshUpstreamChannel(c, service.RefreshUpstreamChannelKeys)
}

func RefreshUpstreamChannelGroups(c *gin.Context) {
	refreshUpstreamChannel(c, service.RefreshUpstreamChannelGroups)
}

func LinkUpstreamChannelKeys(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	row, _, summary, err := service.LinkUpstreamChannelKeys(c.Request.Context(), id)
	if err != nil {
		var data any
		if row != nil {
			data = buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row))
		}
		respondUpstreamChannelError(c, err, data)
		return
	}
	common.ApiSuccess(c, gin.H{
		"channel": buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)),
		"summary": summary,
	})
}

func UpdateUpstreamChannelKeyGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	keyID, err := strconv.ParseInt(c.Param("key_id"), 10, 64)
	if err != nil || keyID <= 0 {
		common.ApiError(c, errInvalidUpstreamKeyID)
		return
	}
	var request updateUpstreamChannelKeyGroupRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	row, _, err := service.UpdateUpstreamChannelKeyGroup(c.Request.Context(), id, keyID, service.UpstreamKeyGroupUpdate{
		Group:   request.Group,
		GroupID: request.GroupID,
	})
	if err != nil {
		var data any
		if row != nil {
			data = buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row))
		}
		respondUpstreamChannelError(c, err, data)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func refreshUpstreamChannel(c *gin.Context, refresh func(context.Context, int) (*model.UpstreamChannel, service.UpstreamSnapshot, error)) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	row, _, err := refresh(c.Request.Context(), id)
	if err != nil {
		var data any
		if row != nil {
			data = buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row))
		}
		respondUpstreamChannelError(c, err, data)
		return
	}
	common.ApiSuccess(c, buildUpstreamChannelView(row, 0, 0, upstreamChannelInUseKeyCount(row)))
}

func RefreshAllUpstreamChannels(c *gin.Context) {
	refreshed, errorsFound := service.RefreshAllUpstreamChannels(c.Request.Context())
	common.ApiSuccess(c, gin.H{
		"refreshed": refreshed,
		"errors":    errorsFound,
	})
}

func RevealUpstreamChannelKey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	keyID, err := strconv.ParseInt(c.Param("key_id"), 10, 64)
	if err != nil || keyID <= 0 {
		common.ApiError(c, errInvalidUpstreamKeyID)
		return
	}
	key, err := service.RevealUpstreamChannelKey(c.Request.Context(), id, keyID)
	if err != nil {
		respondUpstreamChannelError(c, err, nil)
		return
	}
	common.ApiSuccess(c, gin.H{"key": key})
}

func FetchUpstreamChannelKeyModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	var request fetchUpstreamChannelKeyModelsRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	models, err := service.FetchUpstreamChannelKeyModels(c.Request.Context(), id, request.KeyIDs)
	if err != nil {
		respondUpstreamChannelError(c, err, nil)
		return
	}
	common.ApiSuccess(c, models)
}

func ImportUpstreamChannelKeys(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errInvalidUpstreamChannelID)
		return
	}
	var request importUpstreamChannelKeysRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.ImportUpstreamChannelKeys(c.Request.Context(), id, service.UpstreamKeyImportOptions{
		KeyIDs:     request.KeyIDs,
		Groups:     request.Groups,
		Tag:        request.Tag,
		NamePrefix: request.NamePrefix,
		Priority:   request.Priority,
		Weight:     request.Weight,
		TestModel:  request.TestModel,
		Models:     request.Models,
		AutoBan:    request.AutoBan,
		Remark:     request.Remark,
	})
	if err != nil {
		respondUpstreamChannelError(c, err, nil)
		return
	}
	common.ApiSuccess(c, result)
}

func respondUpstreamChannelError(c *gin.Context, err error, data any) {
	response := gin.H{
		"success": false,
		"message": err.Error(),
	}
	if errorCode := service.UpstreamErrorCode(err); errorCode != "" {
		response["error_code"] = errorCode
	}
	if data != nil {
		response["data"] = data
	}
	c.JSON(http.StatusOK, response)
}

func buildUpstreamChannelView(row *model.UpstreamChannel, sourceChannelCount int, activeSourceChannelCount int, inUseKeyCount int) upstreamChannelView {
	view := upstreamChannelView{
		ID:                       row.Id,
		Name:                     row.Name,
		BaseURL:                  row.BaseURL,
		Provider:                 row.Provider,
		AuthType:                 row.EffectiveAuthType(),
		Priority:                 row.Priority,
		SelectedGroup:            row.SelectedGroup,
		Username:                 row.Username,
		Note:                     row.Note,
		DefaultTestModel:         row.DefaultTestModel,
		HasPassword:              row.HasPassword(),
		SourceChannelCount:       sourceChannelCount,
		ActiveSourceChannelCount: activeSourceChannelCount,
		InUseKeyCount:            inUseKeyCount,
		Balance:                  row.Balance,
		BalanceUpdatedTime:       row.BalanceUpdatedTime,
		BalanceThreshold:         row.BalanceThreshold,
		Multiplier:               row.EffectiveMultiplier(),
		AutoRefreshInterval:      row.AutoRefreshInterval,
		LastSyncTime:             row.LastSyncTime,
		LastError:                row.LastError,
		LastErrorCode:            service.UpstreamErrorCodeFromMessage(row.LastError),
		Status:                   row.Status,
	}
	if row.SnapshotJSON != "" {
		var snapshot service.UpstreamSnapshot
		if err := common.Unmarshal([]byte(row.SnapshotJSON), &snapshot); err == nil {
			service.NormalizeUpstreamSnapshot(&snapshot)
			view.Snapshot = &snapshot
		}
	}
	return view
}

func upstreamChannelInUseKeyCount(row *model.UpstreamChannel) int {
	if row == nil || strings.TrimSpace(row.SnapshotJSON) == "" {
		return 0
	}
	var snapshot service.UpstreamSnapshot
	if err := common.UnmarshalJsonStr(row.SnapshotJSON, &snapshot); err != nil {
		return 0
	}
	service.NormalizeUpstreamSnapshot(&snapshot)
	return service.SummarizeUpstreamKeyLinks(snapshot.Keys).Enabled
}

func validUpstreamChannelMultiplier(multiplier float64) bool {
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier <= 0 || multiplier > 1_000_000_000 {
		return false
	}
	scaled := multiplier * 100
	return math.Abs(scaled-math.Round(scaled)) <= 1e-9
}

func validateUpstreamAuthentication(provider string, authType string, username string, hasCredential bool) error {
	if authType != model.UpstreamAuthTypePassword && authType != model.UpstreamAuthTypeAccessToken {
		return errInvalidUpstreamAuthType
	}
	if authType != model.UpstreamAuthTypeAccessToken {
		return nil
	}
	if provider != service.UpstreamProviderNewAPI && provider != service.UpstreamProviderSub2API {
		return errUpstreamAccessTokenProvider
	}
	if provider == service.UpstreamProviderNewAPI {
		userID, err := strconv.ParseInt(strings.TrimSpace(username), 10, 64)
		if err != nil || userID <= 0 {
			return errInvalidUpstreamUserID
		}
	}
	if !hasCredential {
		return errUpstreamCredentialRequired
	}
	return nil
}
