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
		upstreamRoute.PUT("/:id", controller.UpdateUpstreamChannelConfig)
		upstreamRoute.DELETE("/:id", controller.DeleteUpstreamChannel)
		upstreamRoute.POST("/:id/pin", controller.PinUpstreamChannel)
		upstreamRoute.PATCH("/:id/note", controller.UpdateUpstreamChannelNote)
		upstreamRoute.PATCH("/:id/selected-group", controller.UpdateUpstreamChannelSelectedGroup)
		upstreamRoute.POST("/:id/refresh", controller.RefreshUpstreamChannel)
		upstreamRoute.POST("/:id/keys/import", controller.ImportUpstreamChannelKeys)
		upstreamRoute.POST("/:id/keys/models", controller.FetchUpstreamChannelKeyModels)
		upstreamRoute.POST("/:id/keys/:key_id", controller.RevealUpstreamChannelKey)
	}
}
