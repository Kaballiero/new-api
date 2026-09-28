package router

import (
	"embed"
	"net/http"
	"path"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS      embed.FS
	IndexPage    []byte
	OpenAPISpecs embed.FS
}

// apiPathPrefixes are the request-path prefixes owned by the API, relay and
// task surfaces. Anything unmatched below them is a missing endpoint and must
// answer with the API error envelope, never with the dashboard shell.
// "/dashboard" itself is a dashboard page, so only its billing subtree counts.
var apiPathPrefixes = []string{
	"/api",
	"/assets",
	"/v1",
	"/v1beta",
	"/mj",
	"/suno",
	"/pg",
	"/dashboard/billing",
}

// isAPIPath reports whether a request path belongs to an API surface.
// The path is normalized first: reverse proxies match their location rules on
// the normalized path but forward the raw one, so "//v1/models" or "/./v1/x"
// reach the router unmatched and would otherwise fall through to the shell.
func isAPIPath(requestPath string) bool {
	if requestPath == "" {
		return false
	}
	cleaned := path.Clean(requestPath)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	for _, prefix := range apiPathPrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return true
		}
	}
	return false
}

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")

	router.NoRoute(
		pluginDispatcher,
		middleware.RouteTag("web"),
		gzip.Gzip(gzip.DefaultCompression),
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(),
		middleware.Cache(),
		static.Serve("/", frontendFS),
		func(c *gin.Context) {
			if isAPIPath(c.Request.URL.Path) {
				controller.RelayNotFound(c)
				return
			}
			c.Header("Cache-Control", "no-cache")
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
		},
	)
}
