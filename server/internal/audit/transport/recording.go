package transport

import (
	"context"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/audit/domain"

	"github.com/gin-gonic/gin"
)

type Recording struct {
	execute func(context.Context, domain.Recording) error
	failed  func(context.Context, error)
	now     func() int64
	newID   func() string
}

func NewRecording(execute func(context.Context, domain.Recording) error, failed func(context.Context, error),
	now func() int64, newID func() string) Recording {
	return Recording{execute: execute, failed: failed, now: now, newID: newID}
}

func Actor(c *gin.Context) domain.Actor {
	if c == nil {
		return domain.Actor{}
	}
	return domain.Actor{ID: c.GetInt("id"), Username: c.GetString("username"), Role: c.GetInt("role")}
}

func Operator(c *gin.Context) *domain.Operator {
	actor := Actor(c)
	return &domain.Operator{ID: actor.ID, Username: actor.Username, Role: actor.Role, AuthMethod: authMethod(c)}
}

func authMethod(c *gin.Context) string {
	if c != nil && c.GetBool("use_access_token") {
		return "access_token"
	}
	return "session"
}

func requestFacts(c *gin.Context) domain.RequestFacts {
	if c == nil || c.Request == nil {
		return domain.RequestFacts{}
	}
	params := map[string]string{}
	for _, p := range c.Params {
		params[p.Key] = p.Value
	}
	status := 0
	if c.Writer != nil {
		status = c.Writer.Status()
	}
	return domain.RequestFacts{Present: true, RequestID: c.GetString(common.RequestIdKey), IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		Method: c.Request.Method, Route: c.FullPath(), AuthMethod: authMethod(c), Status: status, RouteParams: params}
}

func (r Recording) complete(c *gin.Context, in domain.Recording) {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	in.Request = requestFacts(c)
	if err := r.execute(ctx, in); err != nil {
		r.failed(ctx, err)
	}
}

func (r Recording) Manage(c *gin.Context, target int, action string, params map[string]any) {
	r.complete(c, domain.Recording{Source: domain.Management, Actor: Actor(c), TargetID: target, Action: action, Parameters: params})
	MarkLogged(c)
}

func (r Recording) Security(c *gin.Context, user int, action string, params map[string]any) {
	code := ""
	if c != nil {
		code = c.GetString("security_error_code")
	}
	r.complete(c, domain.Recording{Source: domain.Security, Actor: Actor(c), TargetID: user, Action: action, Parameters: params, SecurityCode: code})
}

func (r Recording) Login(c *gin.Context, actor domain.Actor, method string) {
	verification := ""
	if c != nil {
		verification = c.GetString("login_verification_method")
	}
	r.complete(c, domain.Recording{Source: domain.Login, Actor: actor, LoginMethod: method, VerificationMethod: verification})
}

func (r Recording) Quota(c *gin.Context, action string, params map[string]any, success bool) {
	r.complete(c, domain.Recording{Source: domain.QuotaAdjustment, Actor: Actor(c), Action: action, Parameters: params, Success: success})
	MarkLogged(c)
}

func (r Recording) Subscription(c *gin.Context, user int, content string, operator *domain.Operator) {
	r.complete(c, domain.Recording{Source: domain.SubscriptionReset, Actor: Actor(c), TargetID: user, Content: content, Operator: operator})
}

func (r Recording) Integration(c *gin.Context, action string) {
	r.complete(c, domain.Recording{Source: domain.Integration, Actor: Actor(c), Action: action,
		IntegrationID: c.GetString("getapi_integration_id"), TargetID: c.GetInt("getapi_target_user_id")})
}

func MarkLogged(c *gin.Context) {
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
	}
}

func TokenParameters(c *gin.Context) map[string]any {
	params, ok := common.GetContextKeyType[map[string]any](c, constant.ContextKeyTokenAuditParams)
	if !ok {
		params = map[string]any{}
		common.SetContextKey(c, constant.ContextKeyTokenAuditParams, params)
	}
	return params
}

func BatchParameters(c *gin.Context, ids []int) map[string]any {
	params := TokenParameters(c)
	domain.BoundBatchParameters(params, ids)
	return params
}

func (r Recording) BeginAdmin(c *gin.Context) *ResponseWriter {
	if c == nil || c.Request == nil || !domain.IsWriteMethod(c.Request.Method) {
		return nil
	}
	return capture(c)
}

func (r Recording) FinishAdmin(c *gin.Context, writer *ResponseWriter) {
	if writer == nil {
		return
	}
	r.complete(c, domain.Recording{Source: domain.AdminFallback, Actor: Actor(c), Response: writer.result(),
		AlreadyLogged: common.GetContextKeyBool(c, constant.ContextKeyAuditLogged)})
}

func (r Recording) TokenOperation() gin.HandlerFunc {
	return func(c *gin.Context) {
		action, content := domain.TokenAction(c.Request.Method, c.FullPath(), c.Query("status_only"))
		if action == "" {
			c.Next()
			return
		}
		params := map[string]any{}
		if id, err := strconv.Atoi(c.Param("id")); err == nil && id > 0 {
			params["id"] = id
		}
		common.SetContextKey(c, constant.ContextKeyTokenAuditParams, params)
		in := domain.Recording{Source: domain.TokenOperation, Actor: Actor(c), Action: action, Content: content, Parameters: params}
		writer := capture(c)
		c.Next()
		in.Response = writer.result()
		in.Response.HandlerSucceeded = common.GetContextKeyBool(c, constant.ContextKeyTokenAuditSucceeded)
		r.complete(c, in)
	}
}

const AccessTokenContextKey = "access_token_request_audit"

type accessTokenSnapshot struct {
	in     domain.Recording
	writer *ResponseWriter
}

func (r Recording) BeginAccess(c *gin.Context, actor domain.Actor, tokenRef string) {
	if _, exists := c.Get(AccessTokenContextKey); exists {
		return
	}
	writer := capture(c)
	c.Set(AccessTokenContextKey, &accessTokenSnapshot{in: domain.Recording{Source: domain.AccessToken, Actor: actor, TokenRef: tokenRef,
		CreatedAt: r.now(), EventID: r.newID()}, writer: writer})
}

func (r Recording) FinishAccess(c *gin.Context) {
	value, exists := c.Get(AccessTokenContextKey)
	if !exists {
		return
	}
	snapshot, ok := value.(*accessTokenSnapshot)
	if !ok {
		return
	}
	snapshot.in.Response = snapshot.writer.result()
	r.complete(c, snapshot.in)
}

// Operation records an explicit operator reconciliation with root-only evidence.
func (r Recording) Operation(c *gin.Context, action, content string, params, rootFields map[string]any, status int, success, outcomePresent bool) {
	r.complete(c, domain.Recording{Source: domain.OperatorReconciliation, Actor: Actor(c), Action: action, Content: content,
		Parameters: params, RootFields: rootFields, Status: status, Success: success, OutcomePresent: outcomePresent})
	MarkLogged(c)
}
