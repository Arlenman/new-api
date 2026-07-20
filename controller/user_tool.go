package controller

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxUserToolSyncRequestBytes       = 32 * 1024 * 1024
	maxUserToolMutations              = 500
	defaultUserToolBootstrapPageItems = 100
	maxUserToolBootstrapPageItems     = 200
	maxUserToolBootstrapPayloadBytes  = 4 * 1024 * 1024
)

type userToolMutationRequest struct {
	ClientMutationID string          `json:"client_mutation_id"`
	Kind             string          `json:"kind"`
	Key              string          `json:"key"`
	SchemaVersion    int             `json:"schema_version"`
	BaseRevision     int64           `json:"base_revision"`
	Status           string          `json:"status"`
	Payload          model.JSONValue `json:"payload"`
	AssetIDs         []string        `json:"asset_ids"`
	CreatedAt        int64           `json:"created_at"`
	Deleted          bool            `json:"deleted"`
}

type userToolSyncRequest struct {
	Mutations []userToolMutationRequest `json:"mutations"`
}

type userToolPreferenceRequest struct {
	SelectedTokenID int `json:"selected_token_id"`
}

type userToolRuntimeSessionRequest struct {
	TokenID int `json:"token_id"`
}

type userToolItemResponse struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Key           string          `json:"key"`
	SchemaVersion int             `json:"schema_version"`
	Revision      int64           `json:"revision"`
	Status        string          `json:"status"`
	Payload       model.JSONValue `json:"payload"`
	AssetIDs      []string        `json:"asset_ids"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     int64           `json:"updated_at"`
	Deleted       bool            `json:"deleted"`
}

type userToolAssetResponse struct {
	ID          string `json:"id"`
	Sha256      string `json:"sha256"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type userToolMutationResponse struct {
	ClientMutationID string                `json:"client_mutation_id"`
	Kind             string                `json:"kind"`
	Key              string                `json:"key"`
	Result           string                `json:"result"`
	Message          string                `json:"message,omitempty"`
	Item             *userToolItemResponse `json:"item,omitempty"`
}

type userToolTokenResponse struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	MaskedKey    string `json:"masked_key"`
	Group        string `json:"group"`
	DisplayLabel string `json:"display_label"`
}

func GetUserToolBootstrap(c *gin.Context) {
	userID, tool, ok := userToolRequestScope(c)
	if !ok {
		return
	}
	afterID := strings.TrimSpace(c.Query("after_id"))
	if len(afterID) > 64 {
		common.ApiErrorMsg(c, "invalid bootstrap cursor")
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", strconv.Itoa(defaultUserToolBootstrapPageItems))))
	if err != nil || limit <= 0 || limit > maxUserToolBootstrapPageItems {
		common.ApiErrorMsg(c, "invalid bootstrap limit")
		return
	}

	snapshotCursor := int64(0)
	if rawSnapshotCursor, exists := c.GetQuery("snapshot_cursor"); exists {
		snapshotCursor, err = strconv.ParseInt(strings.TrimSpace(rawSnapshotCursor), 10, 64)
		if err != nil || snapshotCursor < 0 {
			common.ApiErrorMsg(c, "invalid bootstrap snapshot cursor")
			return
		}
	} else {
		snapshotCursor, err = model.MaxUserToolChangeID(userID, tool)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	items, hasMore, err := model.ListUserToolItemsPage(userID, tool, true, afterID, limit, maxUserToolBootstrapPayloadBytes)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	itemResponses, assets, err := buildUserToolItemResponses(userID, items)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	preference, err := model.GetUserToolPreference(userID, tool)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	nextAfterID := ""
	if len(items) > 0 {
		nextAfterID = items[len(items)-1].ID
	}
	common.ApiSuccess(c, gin.H{
		"items":         itemResponses,
		"assets":        assets,
		"cursor":        snapshotCursor,
		"next_after_id": nextAfterID,
		"has_more":      hasMore,
		"preference":    gin.H{"selected_token_id": preference.SelectedTokenID, "updated_at": preference.UpdatedTime},
	})
}

func GetUserToolChanges(c *gin.Context) {
	userID, tool, ok := userToolRequestScope(c)
	if !ok {
		return
	}
	cursor, err := strconv.ParseInt(strings.TrimSpace(c.DefaultQuery("cursor", "0")), 10, 64)
	if err != nil || cursor < 0 {
		common.ApiErrorMsg(c, "invalid change cursor")
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("limit", "500")))
	if err != nil || limit <= 0 || limit > model.MaxUserToolChangePageSize {
		common.ApiErrorMsg(c, "invalid change limit")
		return
	}
	changes, err := model.ListUserToolChanges(userID, tool, cursor, limit+1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	hasMore := len(changes) > limit
	if hasMore {
		changes = changes[:limit]
	}
	itemIDs := make([]string, 0, len(changes))
	itemIDSet := make(map[string]struct{}, len(changes))
	nextCursor := cursor
	for _, change := range changes {
		if _, exists := itemIDSet[change.ItemID]; !exists {
			itemIDSet[change.ItemID] = struct{}{}
			itemIDs = append(itemIDs, change.ItemID)
		}
		if change.ID > nextCursor {
			nextCursor = change.ID
		}
	}
	items, err := model.GetUserToolItemsByIDs(userID, tool, itemIDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	itemResponses, assets, err := buildUserToolItemResponses(userID, items)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"changes":     changes,
		"items":       itemResponses,
		"assets":      assets,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
	})
}

func SyncUserTool(c *gin.Context) {
	userID, tool, ok := userToolRequestScope(c)
	if !ok {
		return
	}
	if c.Request.ContentLength > maxUserToolSyncRequestBytes {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUserToolSyncRequestBytes)
	var request userToolSyncRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		common.ApiError(c, err)
		return
	}
	if len(request.Mutations) == 0 || len(request.Mutations) > maxUserToolMutations {
		common.ApiErrorMsg(c, fmt.Sprintf("mutations must contain between 1 and %d items", maxUserToolMutations))
		return
	}
	responses := make([]userToolMutationResponse, 0, len(request.Mutations))
	for _, mutation := range request.Mutations {
		modelMutation := model.UserToolMutation{
			ClientMutationID: mutation.ClientMutationID,
			Kind:             mutation.Kind,
			ItemKey:          mutation.Key,
			SchemaVersion:    mutation.SchemaVersion,
			BaseRevision:     mutation.BaseRevision,
			Status:           mutation.Status,
			Payload:          mutation.Payload,
			AssetIDs:         mutation.AssetIDs,
			CreatedTime:      mutation.CreatedAt,
			Deleted:          mutation.Deleted,
		}
		response := userToolMutationResponse{
			ClientMutationID: strings.TrimSpace(mutation.ClientMutationID),
			Kind:             strings.TrimSpace(mutation.Kind),
			Key:              strings.TrimSpace(mutation.Key),
		}
		if err := model.ValidateUserToolMutation(tool, modelMutation); err != nil {
			response.Result = "error"
			response.Message = err.Error()
			responses = append(responses, response)
			continue
		}
		result, err := model.ApplyUserToolMutation(userID, tool, modelMutation)
		if err != nil {
			response.Result = "error"
			switch {
			case errors.Is(err, model.ErrUserToolMutationIDReused), errors.Is(err, model.ErrUserToolAssetUnavailable):
				response.Message = err.Error()
			default:
				common.SysError("failed to apply user tool mutation: " + err.Error())
				response.Message = "mutation could not be applied"
			}
			responses = append(responses, response)
			continue
		}
		response.Result = result.Result
		response.Message = result.Message
		if result.Item != nil {
			response.Item = buildUserToolItemResponse(*result.Item, result.AssetIDs)
		}
		responses = append(responses, response)
	}
	cursor, err := model.MaxUserToolChangeID(userID, tool)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"results": responses, "cursor": cursor})
}

func GetUserToolPreferences(c *gin.Context) {
	userID, tool, ok := userToolRequestScope(c)
	if !ok {
		return
	}
	preference, err := model.GetUserToolPreference(userID, tool)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"selected_token_id": preference.SelectedTokenID, "updated_at": preference.UpdatedTime})
}

func UpdateUserToolPreferences(c *gin.Context) {
	userID, tool, ok := userToolRequestScope(c)
	if !ok {
		return
	}
	var request userToolPreferenceRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	preference, err := model.SaveUserToolPreference(userID, tool, request.SelectedTokenID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"selected_token_id": preference.SelectedTokenID, "updated_at": preference.UpdatedTime})
}

func CreateUserToolRuntimeSession(c *gin.Context) {
	userID, tool, ok := userToolRequestScope(c)
	if !ok {
		return
	}
	var request userToolRuntimeSessionRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	tokenID := request.TokenID
	if tokenID == 0 {
		preference, err := model.GetUserToolPreference(userID, tool)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		tokenID = preference.SelectedTokenID
	}
	credential, session, err := model.NewUserToolRuntimeSession(userID, tokenID, tool)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.ValidateUserToolToken(userID, tokenID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	displayName := strings.TrimSpace(token.Name)
	if displayName == "" {
		displayName = token.GetMaskedKey()
	}
	displayGroup := strings.TrimSpace(token.Group)
	displayLabel := displayName
	if displayGroup != "" {
		displayLabel += " · " + displayGroup
	}
	common.ApiSuccess(c, gin.H{
		"credential": credential,
		"expires_at": session.ExpiresAt,
		"token": userToolTokenResponse{
			ID:           token.Id,
			Name:         token.Name,
			MaskedKey:    token.GetMaskedKey(),
			Group:        token.Group,
			DisplayLabel: displayLabel,
		},
	})
}

func UploadUserToolAsset(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, model.MaxUserToolAssetBytes+1)
	filename := strings.TrimSpace(c.GetHeader("X-File-Name"))
	if filename == "" {
		filename = strings.TrimSpace(c.Query("filename"))
	}
	asset, err := model.StoreUserToolAsset(
		userID,
		filename,
		c.GetHeader("Content-Type"),
		c.GetHeader("X-Content-SHA256"),
		c.Request.Body,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildUserToolAssetResponse(*asset))
}

func GetUserToolAssetContent(c *gin.Context) {
	userID := c.GetInt("id")
	asset, absolutePath, err := model.ResolveUserToolAsset(userID, c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	file, err := os.Open(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	etag := `"` + asset.Sha256 + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")
	c.Header("X-Content-Type-Options", "nosniff")
	contentType := model.NormalizeUserToolAssetContentType(asset.ContentType)
	dispositionType := "attachment"
	if (strings.HasPrefix(contentType, "image/") && contentType != "image/svg+xml") ||
		strings.HasPrefix(contentType, "audio/") || strings.HasPrefix(contentType, "video/") {
		dispositionType = "inline"
	}
	if disposition := mime.FormatMediaType(dispositionType, map[string]string{"filename": asset.Filename}); disposition != "" {
		c.Header("Content-Disposition", disposition)
	}
	c.Header("Content-Type", contentType)
	if strings.TrimSpace(c.GetHeader("If-None-Match")) == etag {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}
	http.ServeContent(c.Writer, c.Request, asset.Filename, time.UnixMilli(asset.UpdatedTime), file)
}

func userToolRequestScope(c *gin.Context) (int, string, bool) {
	userID := c.GetInt("id")
	tool := strings.TrimSpace(c.Param("tool"))
	if userID <= 0 || !model.IsSupportedUserTool(tool) {
		common.ApiErrorMsg(c, "unsupported user tool")
		return 0, "", false
	}
	return userID, tool, true
}

func buildUserToolItemResponses(userID int, items []model.UserToolItem) ([]userToolItemResponse, []userToolAssetResponse, error) {
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	itemAssetIDs, err := model.GetUserToolItemAssetIDsByItemIDs(userID, itemIDs)
	if err != nil {
		return nil, nil, err
	}

	responses := make([]userToolItemResponse, 0, len(items))
	assetIDs := make(map[string]struct{})
	for _, item := range items {
		ids := itemAssetIDs[item.ID]
		for _, assetID := range ids {
			assetIDs[assetID] = struct{}{}
		}
		responses = append(responses, *buildUserToolItemResponse(item, ids))
	}
	assets, err := model.GetUserToolAssetsByIDs(userID, assetIDs)
	if err != nil {
		return nil, nil, err
	}
	assetResponses := make([]userToolAssetResponse, 0, len(assets))
	for _, asset := range assets {
		assetResponses = append(assetResponses, buildUserToolAssetResponse(asset))
	}
	return responses, assetResponses, nil
}

func buildUserToolItemResponse(item model.UserToolItem, assetIDs []string) *userToolItemResponse {
	if assetIDs == nil {
		assetIDs = []string{}
	}
	return &userToolItemResponse{
		ID:            item.ID,
		Kind:          item.Kind,
		Key:           item.ItemKey,
		SchemaVersion: item.SchemaVersion,
		Revision:      item.Revision,
		Status:        item.Status,
		Payload:       item.Payload,
		AssetIDs:      assetIDs,
		CreatedAt:     item.CreatedTime,
		UpdatedAt:     item.UpdatedTime,
		Deleted:       item.Deleted,
	}
}

func buildUserToolAssetResponse(asset model.UserToolAsset) userToolAssetResponse {
	return userToolAssetResponse{
		ID:          asset.ID,
		Sha256:      asset.Sha256,
		Filename:    asset.Filename,
		ContentType: asset.ContentType,
		SizeBytes:   asset.SizeBytes,
		CreatedAt:   asset.CreatedTime,
		UpdatedAt:   asset.UpdatedTime,
	}
}
