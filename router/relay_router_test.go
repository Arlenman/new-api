package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetRelayRouterRegistersPlaygroundRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	routes := make(map[string]gin.RouteInfo)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = route
	}

	tests := []struct {
		method  string
		path    string
		handler string
	}{
		{method: http.MethodPost, path: "/pg/images/generations", handler: "controller.PlaygroundImage"},
		{method: http.MethodPost, path: "/pg/images/edits", handler: "controller.PlaygroundImage"},
		{method: http.MethodPost, path: "/pg/v1/images/generations", handler: "controller.PlaygroundImage"},
		{method: http.MethodPost, path: "/pg/v1/images/edits", handler: "controller.PlaygroundImage"},
		{method: http.MethodPost, path: "/pg/responses", handler: "controller.PlaygroundResponses"},
		{method: http.MethodPost, path: "/pg/v1/responses", handler: "controller.PlaygroundResponses"},
		{method: http.MethodGet, path: "/pg/image-tasks/:task_id", handler: "controller.GetPlaygroundImageTask"},
		{method: http.MethodGet, path: "/pg/image-files/:id/content", handler: "controller.GetManagedPlaygroundImageFileContent"},
	}

	for _, test := range tests {
		route, ok := routes[test.method+" "+test.path]
		require.True(t, ok, "missing playground route %s", test.path)
		assert.Contains(t, route.Handler, test.handler)
	}
}
