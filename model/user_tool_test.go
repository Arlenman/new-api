package model

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserToolMutationOwnershipConflictAndTombstone(t *testing.T) {
	truncateTables(t)

	created, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "create-canvas-1", Kind: "project", ItemKey: "canvas-1", SchemaVersion: 1, Payload: JSONValue(`{"name":"first"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Item)
	assert.EqualValues(t, 1, created.Item.Revision)

	_, err = GetUserToolItem(202, UserToolInfiniteCanvas, "project", "canvas-1")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	updated, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "update-canvas-1", Kind: "project", ItemKey: "canvas-1", SchemaVersion: 1, BaseRevision: 1, Payload: JSONValue(`{"name":"second"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolMutationResultApplied, updated.Result)
	assert.EqualValues(t, 2, updated.Item.Revision)

	deleted, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "delete-canvas-1", Kind: "project", ItemKey: "canvas-1", SchemaVersion: 1, BaseRevision: 2, Deleted: true, Payload: JSONValue(`{"name":"second"}`),
	})
	require.NoError(t, err)
	assert.True(t, deleted.Item.Deleted)
	assert.EqualValues(t, 3, deleted.Item.Revision)

	stale, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "stale-canvas-1", Kind: "project", ItemKey: "canvas-1", SchemaVersion: 1, BaseRevision: 2, Payload: JSONValue(`{"name":"stale"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolMutationResultConflict, stale.Result)
	assert.True(t, stale.Item.Deleted)
	assert.EqualValues(t, 3, stale.Item.Revision)

	sameRevisionRestore, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "restore-canvas-1", Kind: "project", ItemKey: "canvas-1", SchemaVersion: 1, BaseRevision: 3, Payload: JSONValue(`{"name":"restored"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolMutationResultConflict, sameRevisionRestore.Result)
	assert.True(t, sameRevisionRestore.Item.Deleted)
	assert.EqualValues(t, 3, sameRevisionRestore.Item.Revision)
}

func TestUserToolTerminalGenerationStateDoesNotRegress(t *testing.T) {
	truncateTables(t)

	created, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "complete-task-1", Kind: "generation-task", ItemKey: "task-1", Status: "completed", Payload: JSONValue(`{"result":"ok"}`),
	})
	require.NoError(t, err)

	result, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "regress-task-1", Kind: "generation-task", ItemKey: "task-1", BaseRevision: created.Item.Revision, Status: "running", Payload: JSONValue(`{"progress":50}`),
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolMutationResultConflict, result.Result)
	assert.Equal(t, "completed", result.Item.Status)
	assert.JSONEq(t, `{"result":"ok"}`, string(result.Item.Payload))
}

func TestUserToolAssetDeduplicatesPerUserAndRejectsCrossUserAccess(t *testing.T) {
	truncateTables(t)
	assetDir := t.TempDir()
	t.Setenv("USER_TOOL_ASSET_DIR", assetDir)

	first, err := StoreUserToolAsset(101, "first.png", "image/png", "", bytes.NewBufferString("same-image"))
	require.NoError(t, err)
	second, err := StoreUserToolAsset(101, "second.png", "image/png", first.Sha256, bytes.NewBufferString("same-image"))
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)

	third, err := StoreUserToolAsset(202, "third.png", "image/png", "", bytes.NewBufferString("same-image"))
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, third.ID)

	_, path, err := ResolveUserToolAsset(101, first.ID)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(path))
	_, _, err = ResolveUserToolAsset(202, first.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestUserToolAssetReuploadRepairsMissingActiveContent(t *testing.T) {
	truncateTables(t)
	assetDir := t.TempDir()
	t.Setenv("USER_TOOL_ASSET_DIR", assetDir)

	content := []byte("content-restored-after-local-storage-loss")
	asset, err := StoreUserToolAsset(101, "original.png", "image/png", "", bytes.NewReader(content))
	require.NoError(t, err)
	originalPath := UserToolAssetAbsolutePath(*asset)
	require.NoError(t, os.Remove(originalPath))

	restored, err := StoreUserToolAsset(101, "retry.png", "image/png", asset.Sha256, bytes.NewReader(content))
	require.NoError(t, err)
	assert.Equal(t, asset.ID, restored.ID)
	assert.False(t, restored.Deleted)

	resolved, restoredPath, err := ResolveUserToolAsset(101, asset.ID)
	require.NoError(t, err)
	assert.Equal(t, restored.StoragePath, resolved.StoragePath)
	restoredContent, err := os.ReadFile(restoredPath)
	require.NoError(t, err)
	assert.Equal(t, content, restoredContent)

	var count int64
	require.NoError(t, DB.Model(&UserToolAsset{}).Where("user_id = ? AND sha256 = ?", 101, asset.Sha256).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestUserToolAssetTombstoneSoftDeletesAndReuploadReactivates(t *testing.T) {
	truncateTables(t)
	t.Setenv("USER_TOOL_ASSET_DIR", t.TempDir())

	content := "reusable-image"
	asset, err := StoreUserToolAsset(101, "first.png", "image/png", "", bytes.NewBufferString(content))
	require.NoError(t, err)
	created, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "create-image-asset",
		Kind:             "image",
		ItemKey:          "image-1",
		Payload:          JSONValue(fmt.Sprintf(`{"asset_id":%q}`, asset.ID)),
		AssetIDs:         []string{asset.ID},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Item)

	deleted, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "delete-image-asset",
		Kind:             "image",
		ItemKey:          "image-1",
		BaseRevision:     created.Item.Revision,
		Deleted:          true,
		Payload:          JSONValue(`{}`),
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolMutationResultApplied, deleted.Result)

	var stored UserToolAsset
	require.NoError(t, DB.First(&stored, "id = ?", asset.ID).Error)
	assert.True(t, stored.Deleted)
	_, _, err = ResolveUserToolAsset(101, asset.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	reactivated, err := StoreUserToolAsset(101, "restored.png", "image/png", asset.Sha256, bytes.NewBufferString(content))
	require.NoError(t, err)
	assert.Equal(t, asset.ID, reactivated.ID)
	assert.False(t, reactivated.Deleted)
	_, _, err = ResolveUserToolAsset(101, asset.ID)
	require.NoError(t, err)
}

func TestUserToolAssetRemainsAvailableWhileAnotherItemReferencesIt(t *testing.T) {
	truncateTables(t)
	t.Setenv("USER_TOOL_ASSET_DIR", t.TempDir())

	shared, err := StoreUserToolAsset(101, "shared.png", "image/png", "", bytes.NewBufferString("shared-image"))
	require.NoError(t, err)
	replacement, err := StoreUserToolAsset(101, "replacement.png", "image/png", "", bytes.NewBufferString("replacement-image"))
	require.NoError(t, err)

	first, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "create-shared-image-1", Kind: "image", ItemKey: "shared-image-1",
		Payload: JSONValue(fmt.Sprintf(`{"asset_id":%q}`, shared.ID)), AssetIDs: []string{shared.ID},
	})
	require.NoError(t, err)
	second, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "create-shared-image-2", Kind: "image", ItemKey: "shared-image-2",
		Payload: JSONValue(fmt.Sprintf(`{"asset_id":%q}`, shared.ID)), AssetIDs: []string{shared.ID},
	})
	require.NoError(t, err)

	updated, err := ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "replace-shared-image-1", Kind: "image", ItemKey: "shared-image-1", BaseRevision: first.Item.Revision,
		Payload: JSONValue(fmt.Sprintf(`{"asset_id":%q}`, replacement.ID)), AssetIDs: []string{replacement.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, UserToolMutationResultApplied, updated.Result)
	_, _, err = ResolveUserToolAsset(101, shared.ID)
	require.NoError(t, err)

	_, err = ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "delete-shared-image-2", Kind: "image", ItemKey: "shared-image-2",
		BaseRevision: second.Item.Revision, Deleted: true, Payload: JSONValue(`{}`),
	})
	require.NoError(t, err)
	_, _, err = ResolveUserToolAsset(101, shared.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, _, err = ResolveUserToolAsset(101, replacement.ID)
	require.NoError(t, err)

	_, err = ApplyUserToolMutation(101, UserToolImagePlayground, UserToolMutation{
		ClientMutationID: "delete-replacement-image-1", Kind: "image", ItemKey: "shared-image-1",
		BaseRevision: updated.Item.Revision, Deleted: true, Payload: JSONValue(`{}`),
	})
	require.NoError(t, err)
	_, _, err = ResolveUserToolAsset(101, replacement.ID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestUserToolAssetRejectsStoragePathEscape(t *testing.T) {
	truncateTables(t)
	assetDir := t.TempDir()
	t.Setenv("USER_TOOL_ASSET_DIR", assetDir)

	asset := UserToolAsset{
		ID: "uta_escape", UserID: 101, Sha256: stringsOf("a", 64), Filename: "escape.png",
		ContentType: "image/png", SizeBytes: 1, StoragePath: filepath.Join("..", "escape.png"), CreatedTime: 1, UpdatedTime: 1,
	}
	require.NoError(t, DB.Create(&asset).Error)
	_, _, err := ResolveUserToolAsset(101, asset.ID)
	assert.EqualError(t, err, "invalid asset storage path")
}

func TestUserToolPreferenceAndRuntimeCredentialStayWithinOwner(t *testing.T) {
	truncateTables(t)
	ownerToken := insertUserToolToken(t, 101, "owner-key")
	otherToken := insertUserToolToken(t, 202, "other-key")

	_, err := SaveUserToolPreference(101, UserToolImagePlayground, otherToken.Id)
	assert.ErrorIs(t, err, ErrTokenInvalid)

	preference, err := SaveUserToolPreference(101, UserToolImagePlayground, ownerToken.Id)
	require.NoError(t, err)
	assert.Equal(t, ownerToken.Id, preference.SelectedTokenID)

	credential, session, err := NewUserToolRuntimeSession(101, ownerToken.Id, UserToolImagePlayground)
	require.NoError(t, err)
	assert.NotContains(t, session.ID, credential)
	resolvedSession, resolvedToken, err := ResolveUserToolRuntimeToken(credential)
	require.NoError(t, err)
	assert.Equal(t, session.ID, resolvedSession.ID)
	assert.Equal(t, ownerToken.Id, resolvedToken.Id)

	require.NoError(t, DB.Model(&Token{}).Where("id = ?", ownerToken.Id).Update("status", common.TokenStatusDisabled).Error)
	_, _, err = ResolveUserToolRuntimeToken(credential)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestValidateUserToolTokenEnforcesOwnerAvailabilityAndGroup(t *testing.T) {
	truncateTables(t)

	validToken := insertUserToolToken(t, 101, "valid-owner-key")
	require.NoError(t, DB.Model(validToken).Update("group", "A组").Error)

	otherUserToken := insertUserToolToken(t, 202, "other-user-key")
	disabledToken := insertUserToolToken(t, 101, "disabled-owner-key")
	require.NoError(t, DB.Model(disabledToken).Update("status", common.TokenStatusDisabled).Error)
	expiredToken := insertUserToolToken(t, 101, "expired-owner-key")
	require.NoError(t, DB.Model(expiredToken).Update("expired_time", common.GetTimestamp()-1).Error)
	exhaustedToken := insertUserToolToken(t, 101, "exhausted-owner-key")
	require.NoError(t, DB.Model(exhaustedToken).Updates(map[string]any{
		"unlimited_quota": false,
		"remain_quota":    0,
	}).Error)

	tests := []struct {
		name    string
		userID  int
		tokenID int
		wantErr error
	}{
		{name: "valid owner token", userID: 101, tokenID: validToken.Id},
		{name: "other user token", userID: 101, tokenID: otherUserToken.Id, wantErr: ErrTokenInvalid},
		{name: "disabled token", userID: 101, tokenID: disabledToken.Id, wantErr: ErrTokenInvalid},
		{name: "expired token", userID: 101, tokenID: expiredToken.Id, wantErr: ErrTokenInvalid},
		{name: "exhausted token", userID: 101, tokenID: exhaustedToken.Id, wantErr: ErrTokenInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, err := ValidateUserToolToken(test.userID, test.tokenID)
			if test.wantErr != nil {
				assert.ErrorIs(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, validToken.Id, token.Id)
			assert.Equal(t, "A组", token.Group)
		})
	}

	_, err := SaveUserToolPreference(101, UserToolInfiniteCanvas, disabledToken.Id)
	assert.ErrorIs(t, err, ErrTokenInvalid)
	_, err = SaveUserToolPreference(101, UserToolInfiniteCanvas, expiredToken.Id)
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

func TestUserToolRuntimeCredentialExpires(t *testing.T) {
	truncateTables(t)
	token := insertUserToolToken(t, 101, "expiring-key")
	credential, session, err := NewUserToolRuntimeSession(101, token.Id, UserToolInfiniteCanvas)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&UserToolRuntimeSession{}).Where("id = ?", session.ID).Update("expires_at", time.Now().Add(-time.Minute).UnixMilli()).Error)

	_, err = ResolveUserToolRuntimeSession(credential)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	var count int64
	require.NoError(t, DB.Model(&UserToolRuntimeSession{}).Where("id = ?", session.ID).Count(&count).Error)
	assert.Zero(t, count)
}

func insertUserToolToken(t *testing.T, userID int, key string) *Token {
	t.Helper()
	token := &Token{
		UserId: userID, Key: key, Name: key, Status: common.TokenStatusEnabled,
		CreatedTime: common.GetTimestamp(), ExpiredTime: -1, UnlimitedQuota: true,
	}
	require.NoError(t, DB.Create(token).Error)
	return token
}

func stringsOf(value string, count int) string {
	return strings.Repeat(value, count)
}

func TestUserToolAssetClaimedHashMismatchDoesNotPersist(t *testing.T) {
	truncateTables(t)
	t.Setenv("USER_TOOL_ASSET_DIR", t.TempDir())

	_, err := StoreUserToolAsset(101, "image.png", "image/png", stringsOf("0", 64), bytes.NewBufferString("image"))
	assert.EqualError(t, err, "asset sha256 mismatch")
	var count int64
	require.NoError(t, DB.Model(&UserToolAsset{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUserToolAssetNormalizesMetadataAcrossDatabases(t *testing.T) {
	truncateTables(t)
	t.Setenv("USER_TOOL_ASSET_DIR", t.TempDir())

	asset, err := StoreUserToolAsset(
		101,
		strings.Repeat("图", 300)+".png",
		"Image/PNG; charset=binary",
		"",
		bytes.NewBufferString("image-one"),
	)
	require.NoError(t, err)
	assert.Equal(t, "image/png", asset.ContentType)
	assert.Len(t, []rune(asset.Filename), 255)
	assert.True(t, utf8.ValidString(asset.Filename))

	invalidTypeAsset, err := StoreUserToolAsset(
		101,
		"payload.bin",
		"not a valid content type",
		"",
		bytes.NewBufferString("image-two"),
	)
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", invalidTypeAsset.ContentType)
}

func TestUserToolAssetDirectoryUsesConfiguredLocation(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "nested")
	t.Setenv("USER_TOOL_ASSET_DIR", configured)
	assert.Equal(t, filepath.Clean(configured), UserToolAssetDir())

	oldValue, hadValue := os.LookupEnv("USER_TOOL_ASSET_DIR")
	if hadValue {
		require.NoError(t, os.Setenv("USER_TOOL_ASSET_DIR", oldValue))
	} else {
		require.NoError(t, os.Unsetenv("USER_TOOL_ASSET_DIR"))
	}
}

var _ = errors.Is

func TestUserToolMutationClientIDIsIdempotent(t *testing.T) {
	truncateTables(t)

	mutation := UserToolMutation{
		ClientMutationID: "create-project-once",
		Kind:             "project",
		ItemKey:          "canvas-idempotent",
		SchemaVersion:    1,
		Payload:          JSONValue(`{"name":"Idempotent"}`),
	}
	first, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, mutation)
	require.NoError(t, err)
	require.NotNil(t, first.Item)
	assert.Equal(t, UserToolMutationResultApplied, first.Result)
	assert.False(t, first.Replayed)

	replayed, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, mutation)
	require.NoError(t, err)
	require.NotNil(t, replayed.Item)
	assert.True(t, replayed.Replayed)
	assert.Equal(t, first.Item.ID, replayed.Item.ID)
	assert.Equal(t, first.Item.Revision, replayed.Item.Revision)

	var changeCount int64
	require.NoError(t, DB.Model(&UserToolChange{}).Where("user_id = ? AND tool = ?", 101, UserToolInfiniteCanvas).Count(&changeCount).Error)
	assert.EqualValues(t, 1, changeCount)

	mutation.Payload = JSONValue(`{"name":"Different"}`)
	_, err = ApplyUserToolMutation(101, UserToolInfiniteCanvas, mutation)
	assert.ErrorIs(t, err, ErrUserToolMutationIDReused)

	stored, err := GetUserToolItem(101, UserToolInfiniteCanvas, "project", "canvas-idempotent")
	require.NoError(t, err)
	assert.EqualValues(t, 1, stored.Revision)
	assert.JSONEq(t, `{"name":"Idempotent"}`, string(stored.Payload))
}

func TestUserToolMutationRejectsSensitivePayloadWithoutPersistence(t *testing.T) {
	truncateTables(t)

	tests := []struct {
		name    string
		payload JSONValue
	}{
		{name: "nested api key", payload: JSONValue(`{"provider":{"api_key":"sk-server-side-secret"}}`)},
		{name: "authorization", payload: JSONValue(`{"headers":{"Authorization":"Bearer abcdefghijklmnopqrstuvwxyz"}}`)},
		{name: "runtime credential", payload: JSONValue(`{"bridge":{"runtimeCredential":"utrs_runtime_secret_12345"}}`)},
		{name: "agent token", payload: JSONValue(`{"agent":{"canvas_agent_token":"agent-secret"}}`)},
		{name: "webdav nested url", payload: JSONValue(`{"webdav":{"url":"https://dav.example.test"}}`)},
		{name: "webdav nested username", payload: JSONValue(`{"webdav":{"username":"alice"}}`)},
		{name: "webdav nested password", payload: JSONValue(`{"webdav":{"password":"dav-secret"}}`)},
		{name: "base url", payload: JSONValue(`{"providerBaseURL":"https://api.example.test/v1"}`)},
		{name: "api url", payload: JSONValue(`{"image_api_url":"https://images.example.test/v1"}`)},
		{name: "generic token field", payload: JSONValue(`{"session":{"token":"opaque-secret"}}`)},
		{name: "credential shaped openai key", payload: JSONValue(`{"message":"debug value sk-proj-abcdefghijk1234567890"}`)},
		{name: "credential shaped runtime token", payload: JSONValue(`{"message":"runtime utrs_abcdefghijk123456"}`)},
		{name: "credential shaped bearer token", payload: JSONValue(`{"message":"Bearer abcdefghijklmnopqrstuvwxyz"}`)},
		{name: "credential shaped basic authorization", payload: JSONValue(`{"message":"Basic dXNlcjpwYXNz"}`)},
		{name: "credential query parameter", payload: JSONValue(`{"message":"https://example.test/callback?access_token=secret-value"}`)},
		{name: "json encoded sensitive field", payload: JSONValue(`{"plugin":"{\"apiKey\":\"opaque-secret\"}"}`)},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutation := UserToolMutation{
				ClientMutationID: fmt.Sprintf("sensitive-mutation-%d", index),
				Kind:             "project",
				ItemKey:          fmt.Sprintf("sensitive-item-%d", index),
				Payload:          test.payload,
			}

			_, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, mutation)
			require.ErrorIs(t, err, ErrUserToolSensitivePayload)
			assert.Equal(t, ErrUserToolSensitivePayload.Error(), err.Error())

			var itemCount int64
			require.NoError(t, DB.Model(&UserToolItem{}).Count(&itemCount).Error)
			assert.Zero(t, itemCount)

			var receiptCount int64
			require.NoError(t, DB.Model(&UserToolMutationReceipt{}).Count(&receiptCount).Error)
			assert.Zero(t, receiptCount)
		})
	}
}

func TestUserToolMutationRejectsSensitiveUpdateWithoutChangingItemOrReceipt(t *testing.T) {
	truncateTables(t)

	created, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "create-safe-item",
		Kind:             "project",
		ItemKey:          "safe-item",
		Payload:          JSONValue(`{"name":"Safe"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, created.Item)

	_, err = ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "sensitive-update",
		Kind:             "project",
		ItemKey:          "safe-item",
		BaseRevision:     created.Item.Revision,
		Payload:          JSONValue(`{"name":"Compromised","nested":{"runtimeCredential":"utrs_abcdefghijk123456"}}`),
	})
	require.ErrorIs(t, err, ErrUserToolSensitivePayload)

	stored, err := GetUserToolItem(101, UserToolInfiniteCanvas, "project", "safe-item")
	require.NoError(t, err)
	assert.EqualValues(t, 1, stored.Revision)
	assert.JSONEq(t, `{"name":"Safe"}`, string(stored.Payload))

	var maliciousReceiptCount int64
	require.NoError(t, DB.Model(&UserToolMutationReceipt{}).
		Where("user_id = ? AND tool = ? AND client_mutation_id = ?", 101, UserToolInfiniteCanvas, "sensitive-update").
		Count(&maliciousReceiptCount).Error)
	assert.Zero(t, maliciousReceiptCount)
}

func TestUserToolMutationAllowsOrdinaryTextURLsAndTokenMetrics(t *testing.T) {
	truncateTables(t)

	payload := JSONValue(`{
		"conversation": {
			"message": "Discuss API tokens, Bearer authentication, and Basic authentication without sharing credentials.",
			"documentation_url": "https://docs.example.test/guides/images?lang=en",
			"callback_url": "https://app.example.test/callback?key=canvas-1&state=public-state"
		},
		"usage": {
			"token_count": 128,
			"input_tokens": 64,
			"output_tokens": 64,
			"max_tokens": 512
		},
		"model": "gpt-image-1",
		"status": "completed"
	}`)
	result, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, UserToolMutation{
		ClientMutationID: "ordinary-payload",
		Kind:             "project",
		ItemKey:          "ordinary-item",
		Payload:          payload,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Item)
	assert.JSONEq(t, string(payload), string(result.Item.Payload))

	var receiptCount int64
	require.NoError(t, DB.Model(&UserToolMutationReceipt{}).Count(&receiptCount).Error)
	assert.EqualValues(t, 1, receiptCount)
}

func TestValidateUserToolMutationLimits(t *testing.T) {
	valid := UserToolMutation{
		ClientMutationID: "valid-mutation",
		Kind:             "project",
		ItemKey:          "canvas-1",
		Payload:          JSONValue(`{"name":"Canvas"}`),
	}

	tests := []struct {
		name     string
		mutate   func(*UserToolMutation)
		expected string
	}{
		{name: "missing mutation id", mutate: func(m *UserToolMutation) { m.ClientMutationID = "" }, expected: "invalid client mutation id"},
		{name: "long status", mutate: func(m *UserToolMutation) { m.Status = stringsOf("s", MaxUserToolStatusLength+1) }, expected: "user tool item status is too long"},
		{name: "invalid json", mutate: func(m *UserToolMutation) { m.Payload = JSONValue(`{"name":`) }, expected: "user tool item payload must be valid JSON"},
		{name: "large payload", mutate: func(m *UserToolMutation) { m.Payload = JSONValue(stringsOf("x", MaxUserToolMutationPayloadBytes+1)) }, expected: "user tool item payload is too large"},
		{name: "too many assets", mutate: func(m *UserToolMutation) {
			m.AssetIDs = make([]string, MaxUserToolMutationAssetIDs+1)
			for i := range m.AssetIDs {
				m.AssetIDs[i] = fmt.Sprintf("uta_%d", i)
			}
		}, expected: "too many assets on user tool item"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutation := valid
			test.mutate(&mutation)
			assert.EqualError(t, ValidateUserToolMutation(UserToolInfiniteCanvas, mutation), test.expected)
		})
	}
}

func TestListUserToolItemsPageRespectsPayloadBudgetAndScope(t *testing.T) {
	truncateTables(t)

	payload := JSONValue(`{"blob":"` + stringsOf("x", 320) + `"}`)
	items := []UserToolItem{
		{ID: "uti_001", UserID: 101, Tool: UserToolInfiniteCanvas, Kind: "setting", ItemKey: "alpha", Payload: payload, CreatedTime: 1, UpdatedTime: 1},
		{ID: "uti_002", UserID: 101, Tool: UserToolInfiniteCanvas, Kind: "plugin-record", ItemKey: "beta", Payload: payload, CreatedTime: 2, UpdatedTime: 2},
		{ID: "uti_003", UserID: 101, Tool: UserToolImagePlayground, Kind: "setting", ItemKey: "other-tool", Payload: payload, CreatedTime: 3, UpdatedTime: 3},
		{ID: "uti_004", UserID: 202, Tool: UserToolInfiniteCanvas, Kind: "setting", ItemKey: "other-user", Payload: payload, CreatedTime: 4, UpdatedTime: 4},
	}
	require.NoError(t, DB.Create(&items).Error)

	firstItemBytes := len(items[0].Payload) + len(items[0].ID) + len(items[0].Kind) + len(items[0].ItemKey) + len(items[0].Status) + 256
	page, hasMore, err := ListUserToolItemsPage(101, UserToolInfiniteCanvas, true, "", 10, firstItemBytes+1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "uti_001", page[0].ID)
	assert.True(t, hasMore)

	page, hasMore, err = ListUserToolItemsPage(101, UserToolInfiniteCanvas, true, page[0].ID, 10, firstItemBytes+1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "uti_002", page[0].ID)
	assert.False(t, hasMore)
}

func TestUserToolSettingAndPluginRecordKindsRemainDistinct(t *testing.T) {
	truncateTables(t)

	for _, mutation := range []UserToolMutation{
		{ClientMutationID: "create-setting", Kind: "setting", ItemKey: "shared-key", Payload: JSONValue(`{"theme":"dark"}`)},
		{ClientMutationID: "create-plugin-record", Kind: "plugin-record", ItemKey: "shared-key", Payload: JSONValue(`{"enabled":true}`)},
	} {
		result, err := ApplyUserToolMutation(101, UserToolInfiniteCanvas, mutation)
		require.NoError(t, err)
		require.NotNil(t, result.Item)
		assert.Equal(t, mutation.Kind, result.Item.Kind)
	}

	setting, err := GetUserToolItem(101, UserToolInfiniteCanvas, "setting", "shared-key")
	require.NoError(t, err)
	assert.JSONEq(t, `{"theme":"dark"}`, string(setting.Payload))

	pluginRecord, err := GetUserToolItem(101, UserToolInfiniteCanvas, "plugin-record", "shared-key")
	require.NoError(t, err)
	assert.JSONEq(t, `{"enabled":true}`, string(pluginRecord.Payload))
	assert.NotEqual(t, setting.ID, pluginRecord.ID)
}

func TestUserToolMutationReceiptIsScopedByUserAndTool(t *testing.T) {
	truncateTables(t)

	tests := []struct {
		userID  int
		tool    string
		payload JSONValue
	}{
		{userID: 101, tool: UserToolInfiniteCanvas, payload: JSONValue(`{"scope":"owner-canvas"}`)},
		{userID: 101, tool: UserToolImagePlayground, payload: JSONValue(`{"scope":"owner-image"}`)},
		{userID: 202, tool: UserToolInfiniteCanvas, payload: JSONValue(`{"scope":"other-user-canvas"}`)},
	}
	for _, test := range tests {
		result, err := ApplyUserToolMutation(test.userID, test.tool, UserToolMutation{
			ClientMutationID: "shared-client-mutation-id",
			Kind:             "setting",
			ItemKey:          "shared-key",
			Payload:          test.payload,
		})
		require.NoError(t, err)
		require.NotNil(t, result.Item)
		assert.Equal(t, UserToolMutationResultApplied, result.Result)
		assert.False(t, result.Replayed)
		assert.JSONEq(t, string(test.payload), string(result.Item.Payload))
	}
}
