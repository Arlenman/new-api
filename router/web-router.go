package router

import (
	"embed"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())

	common.SetImagePlaygroundStatus(common.ImagePlaygroundStatus{})
	if dist := strings.TrimSpace(os.Getenv("GPT_IMAGE_PLAYGROUND_DIST")); dist != "" {
		tool, err := loadImagePlayground(dist)
		if err != nil {
			common.SysError("image playground disabled: " + err.Error())
		} else {
			common.SetImagePlaygroundStatus(common.ImagePlaygroundStatus{
				Available: true,
				Version:   tool.buildInfo.Version,
				Commit:    tool.buildInfo.Commit,
				BuiltAt:   tool.buildInfo.BuiltAt,
			})
			router.GET(imagePlaygroundRoute, middleware.DisableCache(), middleware.TokenOrUserAuth(), func(c *gin.Context) {
				c.Redirect(http.StatusTemporaryRedirect, imagePlaygroundRoute+"/")
			})
			router.GET(imagePlaygroundRoute+"/*filepath", middleware.DisableCache(), middleware.TokenOrUserAuth(), tool.serve)
		}
	}

	common.SetInfiniteCanvasStatus(common.InfiniteCanvasStatus{})
	if dist := strings.TrimSpace(os.Getenv("INFINITE_CANVAS_DIST")); dist != "" {
		tool, err := loadInfiniteCanvas(dist)
		if err != nil {
			common.SysError("infinite canvas disabled: " + err.Error())
		} else {
			common.SetInfiniteCanvasStatus(common.InfiniteCanvasStatus{
				Available: true,
				Version:   tool.buildInfo.Version,
				Commit:    tool.buildInfo.Commit,
				BuiltAt:   tool.buildInfo.BuiltAt,
			})
			router.GET(infiniteCanvasRoute, middleware.DisableCache(), middleware.TokenOrUserAuth(), func(c *gin.Context) {
				c.Redirect(http.StatusTemporaryRedirect, infiniteCanvasRoute+"/")
			})
			router.GET(infiniteCanvasRoute+"/*filepath", middleware.DisableCache(), middleware.TokenOrUserAuth(), tool.serve)
		}
	}

	router.Use(static.Serve("/", themeFS))
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") || strings.HasPrefix(c.Request.RequestURI, imagePlaygroundRoute) || strings.HasPrefix(c.Request.RequestURI, infiniteCanvasRoute) {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		if common.GetTheme() == "classic" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
		}
	})
}
