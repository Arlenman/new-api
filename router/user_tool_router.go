package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func registerUserToolRoutes(apiRouter *gin.RouterGroup) {
	userToolRoute := apiRouter.Group("/user-tools")
	userToolRoute.Use(middleware.UserAuth())
	{
		userToolRoute.GET("/:tool/bootstrap", controller.GetUserToolBootstrap)
		userToolRoute.GET("/:tool/changes", controller.GetUserToolChanges)
		userToolRoute.POST("/:tool/sync", controller.SyncUserTool)
		userToolRoute.GET("/:tool/preferences", controller.GetUserToolPreferences)
		userToolRoute.PUT("/:tool/preferences", controller.UpdateUserToolPreferences)
		userToolRoute.POST("/:tool/runtime-session", controller.CreateUserToolRuntimeSession)
		userToolRoute.POST("/assets/uploads", middleware.UserUploadRateLimit(), controller.UploadUserToolAsset)
		userToolRoute.GET("/assets/:id/content", controller.GetUserToolAssetContent)
	}
}
