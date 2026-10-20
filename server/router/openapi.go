package router

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// supportedSpecLocales — runtime allow-list mirroring the generator's
// supportedLocales. Keep in sync with cmd/gen-admin-openapi/i18n.go.
var supportedSpecLocales = map[string]bool{
	"en": true,
	"zh": true,
	"ru": true,
}

const defaultSpecLocale = "en"

// SetOpenAPIRouter exposes:
//   - GET /openapi.json[?lang=en|zh|ru] — localized spec, default en
//   - GET /openapi/ui                    — Swagger UI with language switcher
//
// Spec files (`docs/openapi/api.<locale>.json`) are baked into the binary at
// compile time via go:embed (see main.go). No filesystem reads at request time.
//
// Anonymous access intentionally: the spec documents which endpoints exist
// and their schemas. It does NOT leak credentials or PII. Frontend SDK
// codegen consumes /openapi.json directly during build.
func SetOpenAPIRouter(router *gin.Engine, specs embed.FS) {
	router.GET("/openapi.json", func(c *gin.Context) {
		lang := c.Query("lang")
		if lang == "" || !supportedSpecLocales[lang] {
			lang = defaultSpecLocale
		}
		data, err := specs.ReadFile("docs/openapi/api." + lang + ".json")
		if err != nil {
			// Fallback to the locale-agnostic alias (api.json == en copy).
			data, err = specs.ReadFile("docs/openapi/api.json")
			if err != nil {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
		}
		c.Header("Cache-Control", "public, max-age=300")
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	})
	router.GET("/openapi/ui", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUIHTML))
	})
}

// swaggerUIHTML is a self-contained Swagger UI page. Loads CSS/JS from
// unpkg CDN at the pinned version below. Includes a language selector that
// swaps the spec URL via SwaggerUIBundle's `url` action.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>new-api OpenAPI</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
<style>
  body { margin: 0; }
  #lang-bar {
    position: sticky; top: 0; z-index: 100;
    background: #1b1b1b; color: #fff;
    padding: 8px 16px;
    display: flex; align-items: center; gap: 12px;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    font-size: 14px;
    box-shadow: 0 1px 4px rgba(0,0,0,0.3);
  }
  #lang-bar label { font-weight: 500; }
  #lang-bar select {
    background: #2b2b2b; color: #fff;
    border: 1px solid #444; border-radius: 4px;
    padding: 4px 8px; font-size: 14px;
    cursor: pointer;
  }
  #lang-bar select:hover { background: #333; }
</style>
</head>
<body>
<div id="lang-bar">
  <label for="lang-select">Language / 语言 / Язык:</label>
  <select id="lang-select">
    <option value="en">English</option>
    <option value="zh">中文</option>
    <option value="ru">Русский</option>
  </select>
</div>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-standalone-preset.js"></script>
<script>
(function() {
  // Initial locale: ?lang= query → localStorage → en
  var params = new URLSearchParams(window.location.search);
  var initial = params.get('lang') || localStorage.getItem('openapi-lang') || 'en';
  if (['en', 'zh', 'ru'].indexOf(initial) === -1) initial = 'en';

  function specUrl(lang) {
    return '/openapi.json?lang=' + encodeURIComponent(lang);
  }

  window.ui = SwaggerUIBundle({
    url: specUrl(initial),
    dom_id: '#swagger-ui',
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: 'StandaloneLayout',
    docExpansion: 'list',
    defaultModelsExpandDepth: 1,
    tryItOutEnabled: true,
  });

  var select = document.getElementById('lang-select');
  select.value = initial;
  select.addEventListener('change', function() {
    var newLang = select.value;
    localStorage.setItem('openapi-lang', newLang);
    // Update URL bar without reload (deep links keep working).
    var url = new URL(window.location.href);
    url.searchParams.set('lang', newLang);
    window.history.replaceState({}, '', url);
    // Reload spec — Swagger UI re-renders all operations.
    window.ui.specActions.updateUrl(specUrl(newLang));
    window.ui.specActions.download();
  });
})();
</script>
</body>
</html>`
