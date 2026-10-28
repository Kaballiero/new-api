package transport

import (
	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

func Message(c *gin.Context, key string, data ...map[string]any) string {
	lang := i18n.DefaultLang
	if c != nil && c.Request != nil {
		lang = i18n.GetLangFromContext(c)
	}
	return i18n.Translate(lang, key, data...)
}
