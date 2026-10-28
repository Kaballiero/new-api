package httpapi

import (
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	"github.com/gin-gonic/gin"
)

func recordManageAudit(c *gin.Context, action string, params map[string]any) {
	auditapp.Recording().Manage(c, c.GetInt("id"), action, params)
}

func recordManageAuditFor(c *gin.Context, target int, action string, params map[string]any) {
	auditapp.Recording().Manage(c, target, action, params)
}

func recordUserSecurityAudit(c *gin.Context, user int, action string, params map[string]any) {
	auditapp.Recording().Security(c, user, action, params)
}
