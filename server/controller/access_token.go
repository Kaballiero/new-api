package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/app"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
)

// personalAccessTokensHTTP builds the personal access token handlers once; the
// database is read per request by the injected store.
var personalAccessTokensHTTP = sync.OnceValue(func() app.PersonalAccessTokensHTTP {
	return app.NewPersonalAccessTokensHTTP(recordUserSecurityAudit)
})

func GetAccessTokenStatus(c *gin.Context) { personalAccessTokensHTTP().Status.Handle(c) }

func GenerateAccessToken(c *gin.Context) { personalAccessTokensHTTP().Generate.Handle(c) }

func RevokeAccessToken(c *gin.Context) { personalAccessTokensHTTP().Revoke.Handle(c) }

// BRIDGE: existing router dispatches the two audit scenarios by its registered route.
var auditHTTP = sync.OnceValue(auditapp.NewHTTP)

func GetAuditLogs(c *gin.Context) { auditHTTP().Handle(c) }
