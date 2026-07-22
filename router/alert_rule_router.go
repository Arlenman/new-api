package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerAlertRuleRoutes(apiRouter *gin.RouterGroup) {
	alertRuleRoute := apiRouter.Group("/alert-rules")
	alertRuleRoute.Use(middleware.RootAuth())
	{
		alertRuleRoute.GET("/", controller.GetAlertRules)
		alertRuleRoute.POST("/", controller.CreateAlertRule)
		alertRuleRoute.GET("/providers", controller.GetAlertRuleProviders)
		alertRuleRoute.GET("/config", controller.GetApiNoticeConfig)
		alertRuleRoute.PUT("/config", controller.UpdateApiNoticeConfig)
		alertRuleRoute.POST("/config/reveal", controller.RevealApiNoticeAPIKey)
		alertRuleRoute.POST("/preview", controller.PreviewAlertRule)
		alertRuleRoute.POST("/test-connection", controller.TestAlertRuleConnection)
		alertRuleRoute.POST("/test-send", controller.TestSendAlertRule)
		alertRuleRoute.PUT("/:id", controller.UpdateAlertRule)
		alertRuleRoute.DELETE("/:id", controller.DeleteAlertRule)
	}
}
