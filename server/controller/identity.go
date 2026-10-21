package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// identityErrors writes identity errors for the controller bridges and the
// modules that reuse the security error responses.
var identityErrors = app.NewIdentityErrors()

// identityHTTP builds the identity handlers once; settings and the database are
// read per request by the injected functions. It is assigned in init because
// the login completion callbacks refer back to it.
var identityHTTP func() app.IdentityHTTP

func init() {
	identityHTTP = sync.OnceValue(func() app.IdentityHTTP {
		return app.NewIdentityHTTP(app.IdentityEffects{LoginAudit: recordLoginAudit, CompleteOAuthLogin: setupLogin, ManageAudit: recordManageAuditFor, CompletePasskeyLogin: func(c *gin.Context, user *model.User, version int64) { setupLoginAtAuthVersion(user, version, c) }, CompleteVerifiedLogin: completeVerifiedLoginResponse, SecurityAudit: recordUserSecurityAudit, BuildProfile: buildSelfUserData, CompletePasswordLogin: func(c *gin.Context, account password_login.Account) {
			setupLogin(&model.User{Id: account.ID, Status: account.Status, AuthVersion: account.AuthVersion}, c)
		}})
	})
}
