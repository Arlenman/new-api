package model

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	var result string
	for range count {
		result += value
	}
	return result
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
