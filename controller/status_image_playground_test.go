package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusIncludesImagePlaygroundAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.SetImagePlaygroundStatus(common.ImagePlaygroundStatus{
		Available: true,
		Version:   "0.7.0",
		Commit:    "a10477581b3d43ac98d39777e4445625a9db113d",
		BuiltAt:   "2026-07-17T06:34:02.904Z",
	})
	t.Cleanup(func() {
		common.SetImagePlaygroundStatus(common.ImagePlaygroundStatus{})
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	GetStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Available bool   `json:"image_playground_available"`
			Version   string `json:"image_playground_version"`
			Commit    string `json:"image_playground_commit"`
			BuiltAt   string `json:"image_playground_built_at"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.True(t, payload.Data.Available)
	require.Equal(t, "0.7.0", payload.Data.Version)
	require.Equal(t, "a10477581b3d43ac98d39777e4445625a9db113d", payload.Data.Commit)
	require.Equal(t, "2026-07-17T06:34:02.904Z", payload.Data.BuiltAt)
}
