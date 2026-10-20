package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func NewIdentityHTTP() app.IdentityHTTP {
	return app.NewIdentityHTTP(app.IdentityEffects{WriteSecurityError: writeSecurityOperationError, LoginAudit: recordLoginAudit, CompleteOAuthLogin: setupLogin, ManageAudit: recordManageAuditFor, CompletePasskeyLogin: func(c *gin.Context, user *model.User, version int64) { setupLoginAtAuthVersion(user, version, c) }, CompleteVerifiedLogin: completeVerifiedLoginResponse, SecurityAudit: recordUserSecurityAudit, BuildProfile: buildSelfUserData, CompletePasswordLogin: func(c *gin.Context, account password_login.Account) {
		setupLogin(&model.User{Id: account.ID, Status: account.Status, AuthVersion: account.AuthVersion}, c)
	}})
}
