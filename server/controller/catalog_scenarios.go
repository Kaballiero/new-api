package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

func catalogEffects(c *gin.Context) app.CatalogEffects {
	return app.CatalogEffects{Audit: func(action string, details map[string]any) { recordManageAudit(c, action, details) }}
}
