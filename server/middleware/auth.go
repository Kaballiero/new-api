package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/validate_key"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const authIdentityContextKey = "auth_identity"

type dashboardCredentialKind int

const (
	dashboardCredentialUnmatched dashboardCredentialKind = iota
	dashboardCredentialInternal
	dashboardCredentialPAT
)

func validUserInfo(username string, role int) bool {
	// check username is empty
	if strings.TrimSpace(username) == "" {
		return false
	}
	if !common.IsValidateRole(role) {
		return false
	}
	return true
}

func authHelper(c *gin.Context, minRole int) {
	if _, started := c.Get(accessTokenAuditContextKey); !started {
		defer finishAccessTokenAudit(c)
	}
	user, identity, useAccessToken, err := authenticateDashboardRequest(c)
	if err != nil {
		writeDashboardAuthError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_USER_DISABLED", "message": common.TranslateMessage(c, i18n.MsgAuthUserBanned)})
		return
	}
	if user.Role < minRole {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "AUTH_INSUFFICIENT_PRIVILEGE", "message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege)})
		return
	}
	if !validUserInfo(user.Username, user.Role) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_USER_INVALID", "message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid)})
		return
	}
	setDashboardAuthContext(c, user, identity, useAccessToken)

	// 管理/root 写操作审计兜底：内聚在鉴权链路里，保证任何经过 AdminAuth/RootAuth
	// 的写接口都会自动留痕（无需在路由上单独挂审计中间件，避免漏挂）。
	// handler 内手动埋点者会设置 ContextKeyAuditLogged，finishAdminAudit 据此跳过。
	var auditWriter *auditResponseWriter
	if minRole >= common.RoleAdminUser {
		auditWriter = beginAdminAudit(c)
	}

	c.Next()

	finishAdminAudit(c, auditWriter)
}

func TryUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		if _, started := c.Get(accessTokenAuditContextKey); !started {
			defer finishAccessTokenAudit(c)
		}
		user, identity, credentialKind, err := classifyDashboardCredential(c)
		if err != nil {
			writeDashboardAuthError(c, err)
			return
		}
		if credentialKind != dashboardCredentialUnmatched {
			setDashboardAuthContext(c, user, identity, credentialKind == dashboardCredentialPAT)
		}
		c.Next()
	}
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

// GetAuthIdentity returns a dashboard session identity. PAT-authenticated
// requests intentionally have no SessionID and cannot manage browser sessions.
func GetAuthIdentity(c *gin.Context) (service.AuthIdentity, bool) {
	value, ok := c.Get(authIdentityContextKey)
	if !ok {
		return service.AuthIdentity{}, false
	}
	identity, ok := value.(service.AuthIdentity)
	return identity, ok
}

// GetSessionAuthIdentity returns only identities backed by a live dashboard
// session. PAT-authenticated requests intentionally fail this check.
func GetSessionAuthIdentity(c *gin.Context) (service.AuthIdentity, bool) {
	identity, ok := GetAuthIdentity(c)
	if !ok {
		identity = service.AuthIdentity{
			UserID:          c.GetInt("id"),
			SessionID:       c.GetString("session_id"),
			UserAuthVersion: c.GetInt64("auth_version"),
			SessionVersion:  c.GetInt64("session_version"),
		}
	}
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 {
		return service.AuthIdentity{}, false
	}
	return identity, true
}

func authenticateDashboardRequest(c *gin.Context) (*model.UserBase, service.AuthIdentity, bool, error) {
	user, identity, credentialKind, err := classifyDashboardCredential(c)
	if err != nil {
		return nil, service.AuthIdentity{}, credentialKind == dashboardCredentialPAT, err
	}
	if credentialKind == dashboardCredentialUnmatched {
		return nil, service.AuthIdentity{}, false, service.ErrAuthTokenInvalid
	}
	return user, identity, credentialKind == dashboardCredentialPAT, nil
}

func classifyDashboardCredential(c *gin.Context) (*model.UserBase, service.AuthIdentity, dashboardCredentialKind, error) {
	raw, ok := authorizationToken(c.GetHeader("Authorization"))
	if !ok {
		return nil, service.AuthIdentity{}, dashboardCredentialUnmatched, nil
	}
	identity, internal, err := service.ParseDashboardAccessToken(raw)
	if internal {
		if err != nil {
			return nil, service.AuthIdentity{}, dashboardCredentialInternal, err
		}
		_, user, err := service.ValidateLoginSession(identity)
		if err != nil {
			return nil, service.AuthIdentity{}, dashboardCredentialInternal, err
		}
		return user, identity, dashboardCredentialInternal, nil
	}
	patUser, err := personalAccessTokenAuth().Execute(raw)
	if err != nil {
		return nil, service.AuthIdentity{}, dashboardCredentialPAT, err
	}
	if patUser == nil || patUser.Id <= 0 {
		return nil, service.AuthIdentity{}, dashboardCredentialUnmatched, nil
	}
	beginAccessTokenAudit(c, patUser)
	user, err := model.GetUserCache(patUser.Id)
	if err != nil {
		return nil, service.AuthIdentity{}, dashboardCredentialPAT, err
	}
	return user, service.AuthIdentity{UserID: user.Id, UserAuthVersion: user.AuthVersion}, dashboardCredentialPAT, nil
}

func authorizationToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		header = parts[1]
	} else if len(parts) != 1 {
		return "", false
	}
	return header, header != ""
}

func setDashboardAuthContext(c *gin.Context, user *model.UserBase, identity service.AuthIdentity, useAccessToken bool) {
	c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	c.Set("id", user.Id)
	c.Set("group", user.Group)
	c.Set("user_group", user.Group)
	c.Set("use_access_token", useAccessToken)
	c.Set("session_id", identity.SessionID)
	c.Set("auth_version", identity.UserAuthVersion)
	c.Set("session_version", identity.SessionVersion)
	c.Set(authIdentityContextKey, identity)
	user.WriteContext(c)
}

func writeDashboardAuthError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrAuthTokenExpired) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_TOKEN_EXPIRED", "message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn)})
		return
	}
	if errors.Is(err, service.ErrLoginSessionRevoked) || errors.Is(err, gorm.ErrRecordNotFound) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_SESSION_REVOKED", "message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn)})
		return
	}
	if errors.Is(err, service.ErrAuthTokenInvalid) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "code": "AUTH_UNAUTHORIZED", "message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid)})
		return
	}
	common.SysLog("dashboard authentication error: " + err.Error())
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "code": "AUTH_INTERNAL_ERROR", "message": common.TranslateMessage(c, i18n.MsgDatabaseError)})
}

func RequirePermission(permission authz.Permission) func(c *gin.Context) {
	return func(c *gin.Context) {
		role := c.GetInt("role")
		userID := c.GetInt("id")
		if authz.Can(userID, role, permission) {
			c.Next()
			return
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		c.Abort()
	}
}

func WssAuth(c *gin.Context) {

}

// TokenOrUserAuth allows either session-based user auth or API token auth.
// Used for endpoints that need to be accessible from both the dashboard and API clients.
func TokenOrUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		raw, ok := authorizationToken(c.GetHeader("Authorization"))
		if ok {
			identity, internal, err := service.ParseDashboardAccessToken(raw)
			if !internal {
				TokenAuth()(c)
				return
			}
			if err != nil {
				writeDashboardAuthError(c, err)
				return
			}
			_, user, err := service.ValidateLoginSession(identity)
			if err != nil {
				writeDashboardAuthError(c, err)
				return
			}
			setDashboardAuthContext(c, user, identity, false)
			c.Next()
			return
		}
		// Opaque credentials are relay API keys here, never dashboard PATs.
		TokenAuth()(c)
	}
}

// TokenAuthReadOnly allows expired and exhausted keys to read their usage.
// Explicitly disabled keys and banned owners remain rejected.
func TokenAuthReadOnly() func(c *gin.Context) {
	return func(c *gin.Context) {
		key := c.GetHeader("Authorization")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgTokenNotProvided)})
			return
		}
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.Split(strings.TrimPrefix(key, "sk-"), "-")[0]
		caller, err := gatewayKeyAuth().Execute(c, validate_key.Input{Key: key, ReadOnly: true})
		if err != nil {
			writeGatewayKeyError(c, err, true)
			return
		}
		c.Set("id", caller.Token.UserId)
		c.Set("token_id", caller.Token.Id)
		c.Set("token_key", caller.Token.Key)
		c.Next()
	}
}

func TokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// Read a realtime key from the WebSocket subprotocol.
		if c.Request.Header.Get("Sec-WebSocket-Protocol") != "" {
			// Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.sk-xxx, openai-beta.realtime-v1
			// read sk from Sec-WebSocket-Protocol
			key := c.Request.Header.Get("Sec-WebSocket-Protocol")
			parts := strings.SplitSeq(key, ",")
			for part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "openai-insecure-api-key") {
					key = strings.TrimPrefix(part, "openai-insecure-api-key.")
					break
				}
			}
			c.Request.Header.Set("Authorization", "Bearer "+key)
		}
		// Anthropic-compatible paths also accept x-api-key.
		if strings.Contains(c.Request.URL.Path, "/v1/messages") || strings.Contains(c.Request.URL.Path, "/v1/models") {
			anthropicKey := c.Request.Header.Get("x-api-key")
			if anthropicKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+anthropicKey)
			}
		}
		// Gemini-compatible paths accept query and Google header keys.
		if c.Request.URL.Path == "/v1/models" ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/openai/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
			skKey := c.Query("key")
			if skKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+skKey)
			}
			// The Google header overrides the query key.
			xGoogKey := c.Request.Header.Get("x-goog-api-key")
			if xGoogKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+xGoogKey)
			}
		}
		key := c.Request.Header.Get("Authorization")
		parts := make([]string, 0)
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		if key == "" || key == "midjourney-proxy" {
			key = c.Request.Header.Get("mj-api-secret")
			if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
				key = strings.TrimSpace(key[7:])
			}
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		} else {
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		}
		var channel *string
		if len(parts) > 1 {
			channel = &parts[1]
		}
		caller, err := gatewayKeyAuth().Execute(c, validate_key.Input{Key: key, ClientIP: c.ClientIP(), Channel: channel})
		writeGatewayCaller(c, caller)
		if err != nil {
			writeGatewayKeyError(c, err, false)
			return
		}
		c.Next()
	}
}

// BRIDGE: controller/playground.go and legacy context tests use a trusted key.
func SetupContextForToken(c *gin.Context, token *model.Token, parts ...string) error {
	var channel *string
	if len(parts) > 1 {
		channel = &parts[1]
	}
	result, err := gatewayKeyAuth().PrepareContext(validate_key.ContextInput{Token: record.ToDomain((*record.Token)(token)), Channel: channel})
	if result != nil {
		writeGatewayTokenContext(c, result)
	}
	if err != nil && result != nil {
		writeGatewayKeyError(c, err, false)
	}
	return err
}
