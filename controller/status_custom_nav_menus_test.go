package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusIncludesCustomNavMenus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalOptionMap := common.OptionMap
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{
		"CustomNavMenus":      `[{"id":"docs","title":"Docs","url":"/docs","enabled":true,"placement":"both"}]`,
		"HeaderNavModules":    "",
		"SidebarModulesAdmin": "",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	GetStatus(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			CustomNavMenus string `json:"CustomNavMenus"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.JSONEq(t, `[{"id":"docs","title":"Docs","url":"/docs","enabled":true,"placement":"both"}]`, payload.Data.CustomNavMenus)
}
