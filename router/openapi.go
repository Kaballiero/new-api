package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetOpenAPIRouter exposes two endpoints:
//   - GET /openapi.json — raw OpenAPI 3.0 spec bytes (admin/dashboard API)
//   - GET /openapi/ui    — Swagger UI (HTML, loads Swagger UI from CDN)
//
// The spec bytes are baked into the binary at compile time via go:embed
// (see main.go). No filesystem reads at request time.
//
// Anonymous access intentionally: the spec documents which endpoints exist
// + their schemas. It does NOT leak credentials or PII. Frontend SDK
// codegen (`openapi-ts`) consumes /openapi.json directly during build.
func SetOpenAPIRouter(router *gin.Engine, spec []byte) {
	if len(spec) == 0 {
		return
	}
	router.GET("/openapi.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", spec)
	})
	router.GET("/openapi/ui", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUIHTML))
	})
}

// swaggerUIHTML is a self-contained Swagger UI page. Loads CSS/JS from
// unpkg CDN at the pinned version below. Points at /openapi.json on the
// same origin (no CORS issues).
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>new-api OpenAPI</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
<style>body { margin: 0; }</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-standalone-preset.js"></script>
<script>
window.onload = function() {
  window.ui = SwaggerUIBundle({
    url: '/openapi.json',
    dom_id: '#swagger-ui',
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: 'StandaloneLayout',
  });
};
</script>
</body>
</html>`
