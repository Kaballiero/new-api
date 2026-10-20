package complete_oauth

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"net/http"
)

type Completion interface {
	CallbackOperation
	BindingOperation
	AccountLookup
	AccountRegistration
	ProviderDisplayName() string
	BindingAudit() (bool, int)
	Proof() *dto.SecurityProof
	ProofUserID() int
	AuthenticatedUser() *model.User
}
type HTTP struct {
	factory       func(*gin.Context) Completion
	audit         func(*gin.Context, int, string, map[string]any)
	complete      func(*model.User, *gin.Context)
	externalError func(*gin.Context, error)
}

func NewHTTP(factory func(*gin.Context) Completion, audit func(*gin.Context, int, string, map[string]any), complete func(*model.User, *gin.Context), externalError func(*gin.Context, error)) *HTTP {
	return &HTTP{factory: factory, audit: audit, complete: complete, externalError: externalError}
}
func (h *HTTP) Handle(c *gin.Context) {
	op := h.factory(c)
	bindingSucceeded, warning := false, false
	defer func() {
		required, id := op.BindingAudit()
		if required {
			h.audit(c, id, "user.binding_bind", map[string]any{"provider": c.Param("provider"), "success": bindingSucceeded, "notification_failed": warning})
		}
	}()
	result := NewUsecase(op, op, op, op).Execute()
	if result.Cause != nil {
		if result.Issue == "external" {
			h.externalError(c, result.Cause)
		} else {
			identityhttp.WriteSecurityOperationError(c, result.Cause)
		}
		bindingSucceeded, warning = result.BindingSucceeded, result.NotificationWarning
		return
	}
	switch result.Issue {
	case "provider_unknown":
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthUnknownProvider)})
	case "state":
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
	case "invalid":
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case "provider_disabled":
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, map[string]any{"Provider": op.ProviderDisplayName()})
	case "provider_error":
		c.JSON(http.StatusOK, gin.H{"success": false, "message": result.Message})
	case "already_bound":
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, map[string]any{"Provider": op.ProviderDisplayName()})
	case "bound":
		common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{"action": "bind", "notification_warning": result.NotificationWarning})
		bindingSucceeded, warning = result.BindingSucceeded, result.NotificationWarning
	case "email_taken":
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
	case "deleted":
		common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
	case "registration_disabled":
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
	case "banned":
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
	case "login":
		h.complete(op.AuthenticatedUser(), c)
	case "verified":
		proof := op.Proof()
		h.audit(c, op.ProofUserID(), "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope, "provider": c.Param("provider")})
		common.ApiSuccess(c, proof)
	}
}
