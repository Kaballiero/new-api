package identityhttp

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/adapter/identity/logincompletion"
	"github.com/QuantumNous/new-api/internal/identity/complete_primary_login"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func LoginMethodFromContext(c *gin.Context) string {
	if method := c.GetString("login_method"); method != "" {
		return method
	}
	switch c.FullPath() {
	case "/api/user/login":
		return "password"
	case "/api/user/login/2fa":
		return "2fa"
	case "/api/user/passkey/login/finish":
		return "passkey"
	case "/api/oauth/wechat":
		return "wechat"
	case "/api/oauth/telegram/login":
		return "telegram"
	case "/api/oauth/:provider":
		if provider := c.Param("provider"); provider != "" {
			return "oauth:" + provider
		}
		return "oauth"
	default:
		return "unknown"
	}
}

type PrimaryLogin struct {
	Audit   func(*model.User, *gin.Context)
	Profile func(*model.User) map[string]any
}

func (h PrimaryLogin) Start(user *model.User, c *gin.Context) {
	challenge, err := service.StartLoginVerification(user, LoginMethodFromContext(c))
	if err != nil {
		WriteSecurityOperationError(c, err)
		return
	}
	if challenge != nil {
		SetAuthNoStore(c)
		common.ApiSuccess(c, challenge)
		return
	}
	h.AtVersion(user, user.AuthVersion, c)
}
func (h PrimaryLogin) AtVersion(user *model.User, version int64, c *gin.Context) {
	op := &logincompletion.Operation{Primary: user, Metadata: func() (string, string, string) { return LoginMethodFromContext(c), c.ClientIP(), c.Request.UserAgent() }}
	if failure := complete_primary_login.NewUsecase(op).Execute(version); failure != nil {
		switch failure.Stage {
		case "banned":
			common.ApiErrorI18n(c, i18n.MsgAuthUserBanned)
		case "profile":
			common.ApiError(c, failure.Cause)
		default:
			WriteAuthSessionError(c, failure.Cause)
		}
		return
	}
	h.Write(c, op.Profile, op.Bundle)
}
func (h PrimaryLogin) Write(c *gin.Context, user *model.User, bundle *service.AuthBundle) {
	c.Set("login_method", bundle.Session.LoginMethod)
	model.UpdateUserLastLoginAt(user.Id)
	service.WriteRefreshCookie(c, bundle.RefreshToken)
	SetAuthNoStore(c)
	h.Audit(user, c)
	c.JSON(http.StatusOK, gin.H{"message": "", "success": true, "data": gin.H{"access_token": bundle.AccessToken, "token_type": bundle.TokenType, "access_expires_at": bundle.AccessExpiresAt, "session": bundle.Session, "user": h.Profile(user)}})
}
func (h PrimaryLogin) Verified(c *gin.Context, bundle *service.AuthBundle, method string) {
	identity, err := service.ParseAccessToken(bundle.AccessToken)
	if err != nil {
		WriteAuthSessionError(c, err)
		return
	}
	user, err := model.GetSelfUserById(identity.UserID)
	if err != nil {
		WriteAuthSessionError(c, err)
		return
	}
	c.Set("login_verification_method", method)
	h.Write(c, user, bundle)
}
