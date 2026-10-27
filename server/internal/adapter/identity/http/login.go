package identityhttp

import (
	"errors"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/adapter/identity/logincompletion"
	"github.com/QuantumNous/new-api/internal/identity/complete_primary_login"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/adapter/identity/authtoken"
	"github.com/QuantumNous/new-api/internal/identity/dto"
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
	StartVerification      func(*accountrecord.User, string) (*dto.LoginChallenge, error)
	CreateSession          func(int, string, string, string) (*dto.AuthBundle, error)
	CreateSessionAtVersion func(int, int64, string, string, string) (*dto.AuthBundle, error)
	SQL                    identitystore.Store

	Accounts accountstore.Store

	Usecase complete_primary_login.Usecase
	Audit   func(*accountrecord.User, *gin.Context)
	Profile func(*accountrecord.User) map[string]any
	Errors  transport.Writer
}

func (h PrimaryLogin) Start(user *accountrecord.User, c *gin.Context) {
	challenge, err := h.StartVerification(user, LoginMethodFromContext(c))
	if err != nil {
		h.Errors.WriteError(c, err)
		return
	}
	if challenge != nil {
		SetAuthNoStore(c)
		common.ApiSuccess(c, challenge)
		return
	}
	h.AtVersion(user, user.AuthVersion, c)
}
func (h PrimaryLogin) AtVersion(user *accountrecord.User, version int64, c *gin.Context) {
	op := &logincompletion.Operation{Accounts: h.Accounts, Create: h.CreateSession, CreateAtVersion: h.CreateSessionAtVersion, Primary: user, Metadata: func() (string, string, string) { return LoginMethodFromContext(c), c.ClientIP(), c.Request.UserAgent() }}
	if err := h.Usecase.Execute(op, version); err != nil {
		switch {
		case errors.Is(err, domain.ErrAccountBanned):
			common.ApiErrorI18n(c, i18n.MsgAuthUserBanned)
		case errors.Is(err, domain.ErrInternal):
			common.ApiError(c, err)
		default:
			h.Errors.WriteSessionError(c, err)
		}
		return
	}
	h.Write(c, op.Profile, op.Bundle)
}
func (h PrimaryLogin) Write(c *gin.Context, user *accountrecord.User, bundle *dto.AuthBundle) {
	c.Set("login_method", bundle.Session.LoginMethod)
	h.Accounts.UpdateUserLastLoginAt(user.Id)
	WriteRefreshCookie(h.SQL, c, bundle.RefreshToken)
	SetAuthNoStore(c)
	h.Audit(user, c)
	c.JSON(http.StatusOK, gin.H{"message": "", "success": true, "data": gin.H{"access_token": bundle.AccessToken, "token_type": bundle.TokenType, "access_expires_at": bundle.AccessExpiresAt, "session": bundle.Session, "user": h.Profile(user)}})
}
func (h PrimaryLogin) Verified(c *gin.Context, bundle *dto.AuthBundle, method string) {
	identity, err := authtoken.ParseAccessToken(bundle.AccessToken)
	if err != nil {
		h.Errors.WriteSessionError(c, err)
		return
	}
	user, err := h.Accounts.GetSelfUserById(identity.UserID)
	if err != nil {
		h.Errors.WriteSessionError(c, err)
		return
	}
	c.Set("login_verification_method", method)
	h.Write(c, user, bundle)
}
