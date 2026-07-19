package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSetupPlaygroundTokenContextPreservesSelectedAPIToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_id", 57)
	ctx.Set("token_name", "selected-key")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "GPT分组")

	setupPlaygroundTokenContext(ctx, 1, "default")

	assert.Equal(t, 57, ctx.GetInt("token_id"))
	assert.Equal(t, "selected-key", ctx.GetString("token_name"))
	assert.Equal(t, "GPT分组", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
}

func TestSetupPlaygroundTokenContextCreatesSessionToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	setupPlaygroundTokenContext(ctx, 12, "default")

	assert.Equal(t, 12, ctx.GetInt("id"))
	assert.Equal(t, 0, ctx.GetInt("token_id"))
	assert.Equal(t, "playground-default", ctx.GetString("token_name"))
	assert.Equal(t, "default", common.GetContextKeyString(ctx, constant.ContextKeyTokenGroup))
}
