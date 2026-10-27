package middleware

import (
	"sync"

	"github.com/gin-gonic/gin"

	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/transport"
	patdomain "github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"

	authtoken "github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
)

// BRIDGE: gateway authentication and router middleware retain their existing ordering.
var auditRecording = sync.OnceValue(auditapp.NewRecording)

type auditResponseWriter = transport.ResponseWriter

const accessTokenAuditContextKey = transport.AccessTokenContextKey

func beginAdminAudit(c *gin.Context) *auditResponseWriter {
	return auditRecording().BeginAdmin(c)
}

func finishAdminAudit(c *gin.Context, writer *auditResponseWriter) {
	auditRecording().FinishAdmin(c, writer)
}

func TokenOperationAudit() gin.HandlerFunc {
	return auditRecording().TokenOperation()
}

func AccessTokenAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, present := authorizationToken(c.GetHeader("Authorization"))
		if present {
			_, internal, _ := authtoken.ParseDashboardAccessToken(raw)
			if !internal {
				user, err := personalAccessTokenAuth().Execute(raw)
				if err == nil && user != nil && user.Id > 0 {
					beginAccessTokenAudit(c, user)
					defer finishAccessTokenAudit(c)
				}
			}
		}
		c.Next()
	}
}

func beginAccessTokenAudit(c *gin.Context, user *patdomain.Caller) {
	auditRecording().BeginAccess(c, domain.Actor{ID: user.Id, Username: user.Username, Role: user.Role}, user.TokenRef)
}

func finishAccessTokenAudit(c *gin.Context) {
	auditRecording().FinishAccess(c)
}
