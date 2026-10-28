package httpapi

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	profiledto "github.com/QuantumNous/new-api/internal/accounts/dto"
	accountsadapter "github.com/QuantumNous/new-api/internal/adapter/accounts"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/app"
	accountscore "github.com/QuantumNous/new-api/internal/app/accountscore"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	authz "github.com/QuantumNous/new-api/internal/app/authorizationcore"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"sync"
)

// identityErrors writes identity errors for the controller bridges and the
// modules that reuse the security error responses.
var identityErrors = app.NewIdentityErrors()

// Identity builds the identity handlers once; settings and the database are
// read per request by the injected functions. It is assigned in init because
// the login completion callbacks refer back to it.
var Identity func() app.IdentityHTTP

func init() {
	Identity = sync.OnceValue(func() app.IdentityHTTP {
		return app.NewIdentityHTTP(app.IdentityEffects{LoginAudit: recordLoginAudit, CompleteOAuthLogin: setupLogin, ManageAudit: recordManageAuditFor, CompletePasskeyLogin: func(c *gin.Context, user *model.User, version int64) { setupLoginAtAuthVersion(user, version, c) }, CompleteVerifiedLogin: completeVerifiedLoginResponse, SecurityAudit: recordUserSecurityAudit, BuildProfile: buildSelfUserData, CompletePasswordLogin: func(c *gin.Context, account password_login.Account) {
			setupLogin(&model.User{Id: account.ID, Status: account.Status, AuthVersion: account.AuthVersion}, c)
		}})
	})
}

func recordLoginAudit(user *model.User, c *gin.Context) {
	auditapp.Recording().Login(c, auditdomain.Actor{ID: user.Id, Username: user.Username, Role: user.Role}, identityhttp.LoginMethodFromContext(c))
}

// setupLogin evaluates the shared login policy after primary authentication.
// Only a completed Passkey ceremony may go directly to session issuance.
func setupLogin(user *model.User, c *gin.Context) { Identity().PrimaryLogin.Start(user, c) }

func setupLoginAtAuthVersion(user *model.User, version int64, c *gin.Context) {
	Identity().PrimaryLogin.AtVersion(user, version, c)
}

// buildSelfUserData is the single safe dashboard-user DTO used by GetSelf,
// login and refresh. It intentionally excludes password, management PAT and
// administrator-only remarks.
func buildSelfUserData(user *model.User) map[string]any {
	return profiledto.BuildSelfUserData(repository.ProfileData(user), accountsadapter.SidebarModules, func(id, role int) any { return authz.Capabilities(id, role) }, domain.AdminRoles{Admin: common.RoleAdminUser, Root: common.RoleRootUser})
}

// generateDefaultSidebarConfig returns the role-based default sidebar.
func generateDefaultSidebarConfig(userRole int) string {
	return accountscore.GenerateDefaultSidebarConfigForRole(userRole)
}

func completeVerifiedLoginResponse(c *gin.Context, bundle *identitydto.AuthBundle, method string) {
	Identity().PrimaryLogin.Verified(c, bundle, method)
}
