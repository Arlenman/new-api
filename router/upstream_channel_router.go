package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerUpstreamChannelRoutes(apiRouter *gin.RouterGroup) {
	upstreamRoute := apiRouter.Group("/upstream-channels")
	upstreamRoute.Use(middleware.RootAuth())
	{
		upstreamRoute.GET("/", controller.GetUpstreamChannels)
		upstreamRoute.POST("/", controller.CreateUpstreamChannel)
		upstreamRoute.POST("/refresh", controller.RefreshAllUpstreamChannels)
		upstreamRoute.GET("/priority-schedule", controller.GetUpstreamPrioritySchedule)
		upstreamRoute.PUT("/priority-schedule", controller.UpdateUpstreamPrioritySchedule)
		upstreamRoute.PUT("/:id", controller.UpdateUpstreamChannelConfig)
		upstreamRoute.DELETE("/:id", controller.DeleteUpstreamChannel)
		upstreamRoute.POST("/:id/pin", controller.PinUpstreamChannel)
		upstreamRoute.PATCH("/:id/note", controller.UpdateUpstreamChannelNote)
		upstreamRoute.PATCH("/:id/selected-group", controller.UpdateUpstreamChannelSelectedGroup)
		upstreamRoute.PATCH("/:id/default-test-model", controller.UpdateUpstreamChannelDefaultTestModel)
		upstreamRoute.POST("/:id/refresh", controller.RefreshUpstreamChannel)
		upstreamRoute.POST("/:id/refresh-balance", controller.RefreshUpstreamChannelBalance)
		upstreamRoute.POST("/:id/refresh-keys", controller.RefreshUpstreamChannelKeys)
		upstreamRoute.POST("/:id/refresh-groups", controller.RefreshUpstreamChannelGroups)
		upstreamRoute.POST("/:id/keys/link", controller.LinkUpstreamChannelKeys)
		upstreamRoute.PATCH("/:id/keys/:key_id/group", controller.UpdateUpstreamChannelKeyGroup)
		upstreamRoute.POST("/:id/keys/import", controller.ImportUpstreamChannelKeys)
		upstreamRoute.POST("/:id/keys/models", controller.FetchUpstreamChannelKeyModels)
		upstreamRoute.POST("/:id/keys/:key_id", controller.RevealUpstreamChannelKey)
	}
}
