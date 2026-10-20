package controller

import (
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func RefreshAuth(c *gin.Context)              { NewIdentityHTTP().RefreshSession.Handle(c) }
func AuthLogout(c *gin.Context)               { NewIdentityHTTP().Logout.Handle(c) }
func GetLoginSessions(c *gin.Context)         { NewIdentityHTTP().ListSessions.Handle(c) }
func DeleteLoginSession(c *gin.Context)       { NewIdentityHTTP().RevokeSession.Handle(c) }
func RevokeOtherLoginSessions(c *gin.Context) { NewIdentityHTTP().RevokeOtherSessions.Handle(c) }
func requireBrowserSession(c *gin.Context) (service.AuthIdentity, bool) {
	return identityhttp.RequireBrowserSession(c)
}
func writeAuthSessionError(c *gin.Context, err error)   { identityhttp.WriteAuthSessionError(c, err) }
func setAuthNoStore(c *gin.Context)                     { identityhttp.SetAuthNoStore(c) }
func authRotationData(bundle *service.AuthBundle) gin.H { return identityhttp.AuthRotationData(bundle) }
func dashboardBearer(header string) (string, bool)      { return identityhttp.DashboardBearer(header) }
