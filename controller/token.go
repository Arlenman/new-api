package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

type tokenRequest struct {
	model.Token
	Tags *[]string `json:"tags"`
}

type tokenResponse struct {
	model.Token
	Tags []string            `json:"tags"`
	IPs  []model.TokenIPView `json:"ips,omitempty"`
}

type tokenIPLocationRequestItem struct {
	TokenId int    `json:"token_id"`
	IP      string `json:"ip"`
}

type tokenIPLocationRequest struct {
	Items []tokenIPLocationRequestItem `json:"items"`
}

type tokenIPLocationResult struct {
	TokenId     int    `json:"token_id"`
	IP          string `json:"ip"`
	CountryCode string `json:"country_code,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
	Private     bool   `json:"private,omitempty"`
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
}

const maxTokenIPLocationItems = 50

func canViewTokenIPs(c *gin.Context) bool {
	return c.GetInt("role") == common.RoleRootUser
}

func buildMaskedTokenResponse(token *model.Token, tokenIPs []model.TokenIP) (*tokenResponse, error) {
	if token == nil {
		return nil, nil
	}
	maskedToken := *token
	maskedToken.Key = token.GetMaskedKey()
	tags, err := model.GetTokenTagNames(maskedToken.UserId, maskedToken.Id)
	if err != nil {
		return nil, err
	}
	return &tokenResponse{
		Token: maskedToken,
		Tags:  tags,
		IPs:   model.BuildTokenIPViews(tokenIPs),
	}, nil
}

func buildMaskedTokenResponses(tokens []*model.Token, includeIPs bool) ([]*tokenResponse, error) {
	tokenIPs := make(map[int][]model.TokenIP)
	if includeIPs {
		tokenIds := make([]int, 0, len(tokens))
		for _, token := range tokens {
			tokenIds = append(tokenIds, token.Id)
		}
		var err error
		tokenIPs, err = model.GetTokenIPsByTokenIDs(tokenIds)
		if err != nil {
			return nil, err
		}
	}

	maskedTokens := make([]*tokenResponse, 0, len(tokens))
	for _, token := range tokens {
		maskedToken, err := buildMaskedTokenResponse(token, tokenIPs[token.Id])
		if err != nil {
			return nil, err
		}
		maskedTokens = append(maskedTokens, maskedToken)
	}
	return maskedTokens, nil
}

func GetAllTokens(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	tokens, err := model.GetAllUserTokens(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	total, _ := model.CountUserTokens(userId)
	maskedTokens, err := buildMaskedTokenResponses(tokens, canViewTokenIPs(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(maskedTokens)
	common.ApiSuccess(c, pageInfo)
}

func SearchTokens(c *gin.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := common.GetPageQuery(c)

	tokens, total, err := model.SearchUserTokens(userId, keyword, token, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	maskedTokens, err := buildMaskedTokenResponses(tokens, canViewTokenIPs(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(maskedTokens)
	common.ApiSuccess(c, pageInfo)
}

func GetToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var tokenIPs []model.TokenIP
	if canViewTokenIPs(c) {
		tokenIPMap, loadErr := model.GetTokenIPsByTokenIDs([]int{token.Id})
		if loadErr != nil {
			common.ApiError(c, loadErr)
			return
		}
		tokenIPs = tokenIPMap[token.Id]
	}
	maskedToken, err := buildMaskedTokenResponse(token, tokenIPs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, maskedToken)
}

func GetTokenIPLocations(c *gin.Context) {
	if !canViewTokenIPs(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "仅根用户可查询密钥 IP 地区",
		})
		return
	}

	var req tokenIPLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Items) == 0 || len(req.Items) > maxTokenIPLocationItems {
		common.ApiErrorMsg(c, fmt.Sprintf("每次需要查询 1 到 %d 个密钥 IP", maxTokenIPLocationItems))
		return
	}

	type tokenIPKey struct {
		tokenId int
		ip      string
	}
	keys := make([]tokenIPKey, 0, len(req.Items))
	tokenIds := make([]int, 0, len(req.Items))
	ips := make([]string, 0, len(req.Items))
	seen := make(map[tokenIPKey]struct{}, len(req.Items))
	for _, item := range req.Items {
		ip, err := model.NormalizeIPAddress(item.IP)
		if err != nil || item.TokenId <= 0 {
			common.ApiErrorMsg(c, "密钥 IP 参数无效")
			return
		}
		key := tokenIPKey{tokenId: item.TokenId, ip: ip}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		tokenIds = append(tokenIds, item.TokenId)
		ips = append(ips, ip)
	}

	records, err := model.GetTokenIPsByTokenIDsAndIPs(tokenIds, ips)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	owned := make(map[tokenIPKey]model.TokenIP, len(records))
	for _, record := range records {
		owned[tokenIPKey{tokenId: record.TokenId, ip: record.IP}] = record
	}

	results := make([]tokenIPLocationResult, len(keys))
	group, groupCtx := errgroup.WithContext(c.Request.Context())
	group.SetLimit(5)
	for index, key := range keys {
		index, key := index, key
		results[index] = tokenIPLocationResult{TokenId: key.tokenId, IP: key.ip}
		if _, exists := owned[key]; !exists {
			results[index].Message = "密钥 IP 不存在"
			continue
		}

		group.Go(func() error {
			location, lookupErr := service.LookupIPLocation(groupCtx, key.ip)
			if lookupErr != nil {
				results[index].Message = lookupErr.Error()
				return nil
			}
			results[index].CountryCode = location.CountryCode
			results[index].Region = location.Region
			results[index].City = location.City
			results[index].Private = location.Private
			if location.Private {
				results[index].Success = true
				return nil
			}
			if updateErr := model.UpdateTokenIPLocation(key.tokenId, key.ip, location.CountryCode, location.Region, location.City); updateErr != nil {
				results[index].Message = updateErr.Error()
				return nil
			}
			results[index].Success = true
			return nil
		})
	}
	_ = group.Wait()
	common.ApiSuccess(c, results)
}

func GetTokenKey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"key": token.GetFullKey(),
	})
}

func GetTokenStatus(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := model.GetTokenByIds(tokenId, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"object":          "credit_summary",
		"total_granted":   token.RemainQuota,
		"total_used":      0, // not supported currently
		"total_available": token.RemainQuota,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, err := model.GetTokenByKey(strings.TrimPrefix(tokenKey, "sk-"), false)
	if err != nil {
		common.SysError("failed to get token by key: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": gin.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        token.RemainQuota + token.UsedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"expires_at":           expiredAt,
		},
	})
}

func AddToken(c *gin.Context) {
	req := tokenRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token := req.Token
	if len(token.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	// 非无限额度时，检查额度值是否超出有效范围
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := int((1000000000 * common.QuotaPerUnit))
		if token.RemainQuota > maxQuotaValue {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	// 检查用户令牌数量是否已达上限
	maxTokens := operation_setting.GetMaxUserTokens()
	count, err := model.CountUserTokens(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if int(count) >= maxTokens {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("已达到最大令牌数量限制 (%d)", maxTokens),
		})
		return
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		common.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cleanToken := model.Token{
		UserId:             c.GetInt("id"),
		Name:               token.Name,
		Key:                key,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		AllowIps:           token.AllowIps,
		Group:              token.Group,
		CrossGroupRetry:    token.CrossGroupRetry,
	}
	err = cleanToken.InsertWithTags(req.Tags)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	err := model.DeleteTokenById(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateToken(c *gin.Context) {
	userId := c.GetInt("id")
	statusOnly := c.Query("status_only")
	req := tokenRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token := req.Token
	if len(token.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := int((1000000000 * common.QuotaPerUnit))
		if token.RemainQuota > maxQuotaValue {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	cleanToken, err := model.GetTokenByIds(token.Id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if token.Status == common.TokenStatusEnabled {
		if cleanToken.Status == common.TokenStatusExpired && cleanToken.ExpiredTime <= common.GetTimestamp() && cleanToken.ExpiredTime != -1 {
			common.ApiErrorI18n(c, i18n.MsgTokenExpiredCannotEnable)
			return
		}
		if cleanToken.Status == common.TokenStatusExhausted && cleanToken.RemainQuota <= 0 && !cleanToken.UnlimitedQuota {
			common.ApiErrorI18n(c, i18n.MsgTokenExhaustedCannotEable)
			return
		}
	}
	if statusOnly != "" {
		cleanToken.Status = token.Status
	} else {
		// If you add more fields, please also update token.Update()
		cleanToken.Name = token.Name
		cleanToken.ExpiredTime = token.ExpiredTime
		cleanToken.RemainQuota = token.RemainQuota
		cleanToken.UnlimitedQuota = token.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
		cleanToken.ModelLimits = token.ModelLimits
		cleanToken.AllowIps = token.AllowIps
		cleanToken.Group = token.Group
		cleanToken.CrossGroupRetry = token.CrossGroupRetry
	}
	var tags *[]string
	if statusOnly == "" {
		tags = req.Tags
	}
	err = cleanToken.UpdateWithTags(tags)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var tokenIPs []model.TokenIP
	if canViewTokenIPs(c) {
		tokenIPMap, loadErr := model.GetTokenIPsByTokenIDs([]int{cleanToken.Id})
		if loadErr != nil {
			common.ApiError(c, loadErr)
			return
		}
		tokenIPs = tokenIPMap[cleanToken.Id]
	}
	maskedToken, err := buildMaskedTokenResponse(cleanToken, tokenIPs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    maskedToken,
	})
}

func GetTokenTags(c *gin.Context) {
	tags, err := model.ListTokenTagsByUser(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, tags)
}

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func DeleteTokenBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	count, err := model.BatchDeleteTokens(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func GetTokenKeysBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(tokenBatch.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": 100})
		return
	}
	userId := c.GetInt("id")
	tokens, err := model.GetTokenKeysByIds(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	keysMap := make(map[int]string)
	for _, t := range tokens {
		keysMap[t.Id] = t.GetFullKey()
	}
	common.ApiSuccess(c, gin.H{"keys": keysMap})
}
