package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/transport"
	"github.com/QuantumNous/new-api/model"
)

// BRIDGE: controllers not yet moved to business modules and module audit callbacks.
var auditRecording = sync.OnceValue(auditapp.NewRecording)

// BRIDGE: quota recipient usage logs keep their legacy content and operator envelope.
func auditContentEN(action string, params map[string]any) string {
	return domain.OperationContent(action, params)
}

// BRIDGE: quota recipient usage logs still accept the legacy operator DTO.
func auditOperatorInfo(c *gin.Context) *model.AuditAdminInfo {
	operator := transport.Operator(c)
	return &model.AuditAdminInfo{AdminID: operator.ID, AdminUsername: operator.Username, AdminRole: operator.Role, AuthMethod: operator.AuthMethod}
}

// BRIDGE: channel key reads explicitly suppress fallback recording.
func markAuditLogged(c *gin.Context) { transport.MarkLogged(c) }

// BRIDGE: controller operation callbacks until their owning modules move.
func recordManageAudit(c *gin.Context, action string, params map[string]any) {
	auditRecording().Manage(c, c.GetInt("id"), action, params)
}

func recordManageAuditFor(c *gin.Context, target int, action string, params map[string]any) {
	auditRecording().Manage(c, target, action, params)
}

func recordUserSecurityAudit(c *gin.Context, user int, action string, params map[string]any) {
	auditRecording().Security(c, user, action, params)
}

func tokenAuditParams(c *gin.Context) map[string]any { return transport.TokenParameters(c) }

func tokenBatchAuditParams(c *gin.Context, ids []int) map[string]any {
	return transport.BatchParameters(c, ids)
}
