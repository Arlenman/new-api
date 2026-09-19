package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func parseFlowQuotaTimeRange(c *gin.Context) (int64, int64, bool) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return 0, 0, false
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return 0, 0, false
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenTag := c.Query("token_tag")
	dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username, tokenTag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetQuotaDatesByUser(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}

func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenTag := c.Query("token_tag")
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp, tokenTag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

// tokenTagAnalyticsTimeout 限制单次令牌标签统计的查询时长。
// 该统计依赖日志表聚合，时间范围过大时可能耗时数分钟，超过前置代理的读超时后
// 用户只能看到代理层返回的错误页。这里先于代理超时返回明确提示，让用户缩小范围。
const tokenTagAnalyticsTimeout = 45 * time.Second

func respondTokenTagAnalyticsError(c *gin.Context, ctx context.Context, err error) {
	if ctx.Err() != nil {
		common.SysError("token tag analytics query exceeded " + tokenTagAnalyticsTimeout.String() + ": " + err.Error())
		common.ApiErrorMsg(c, "token tag analytics timed out, please narrow the time range")
		return
	}
	common.ApiError(c, err)
}

func GetAllTokenTagQuotaDates(c *gin.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	includeUntagged, _ := strconv.ParseBool(c.Query("include_untagged"))
	excludeUntagged, _ := strconv.ParseBool(c.Query("exclude_untagged"))
	role := c.GetInt("role")
	ctx, cancel := context.WithTimeout(c.Request.Context(), tokenTagAnalyticsTimeout)
	defer cancel()
	result, err := model.GetTokenTagQuotaAnalyticsWithTrend(ctx, startTimestamp, endTimestamp, username, 0, role, model.TokenTagQuotaFilters{
		IncludedTags:    c.QueryArray("token_tag"),
		ExcludedTags:    c.QueryArray("exclude_token_tag"),
		IncludeUntagged: includeUntagged,
		ExcludeUntagged: excludeUntagged,
	})
	if err != nil {
		respondTokenTagAnalyticsError(c, ctx, err)
		return
	}
	if role == common.RoleRootUser {
		tokenIds := make([]int, 0, len(result.Data))
		seenTokenIds := make(map[int]struct{}, len(result.Data))
		for _, row := range result.Data {
			if row.TokenID <= 0 {
				continue
			}
			if _, exists := seenTokenIds[row.TokenID]; exists {
				continue
			}
			seenTokenIds[row.TokenID] = struct{}{}
			tokenIds = append(tokenIds, row.TokenID)
		}
		tokenIPs, loadErr := model.GetTokenIPsByTokenIDs(tokenIds)
		if loadErr != nil {
			common.ApiError(c, loadErr)
			return
		}
		for _, row := range result.Data {
			row.IPs = model.BuildTokenIPViews(tokenIPs[row.TokenID])
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result.Data,
		"summary": result.Summary,
		"trend":   result.Trend,
	})
}

func GetUserTokenTagQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	includeUntagged, _ := strconv.ParseBool(c.Query("include_untagged"))
	excludeUntagged, _ := strconv.ParseBool(c.Query("exclude_untagged"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), tokenTagAnalyticsTimeout)
	defer cancel()
	result, err := model.GetTokenTagQuotaAnalyticsWithTrend(ctx, startTimestamp, endTimestamp, "", userId, common.RoleCommonUser, model.TokenTagQuotaFilters{
		IncludedTags:    c.QueryArray("token_tag"),
		ExcludedTags:    c.QueryArray("exclude_token_tag"),
		IncludeUntagged: includeUntagged,
		ExcludeUntagged: excludeUntagged,
	})
	if err != nil {
		respondTokenTagAnalyticsError(c, ctx, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result.Data,
		"summary": result.Summary,
		"trend":   result.Trend,
	})
}

func GetTokenTagOptions(c *gin.Context) {
	tags, err := model.ListTokenTagOptions(c.GetInt("id"), c.Query("username"), c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, tags)
}

func GetAllFlowQuotaDates(c *gin.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	tokenTag := c.Query("token_tag")
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, username, 0, c.GetInt("role"), tokenTag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetUserFlowQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	tokenTag := c.Query("token_tag")
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, "", userId, common.RoleCommonUser, tokenTag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}
