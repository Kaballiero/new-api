package complete_oauth

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"
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
	CompleteLogin(*gin.Context)
}
type HTTP struct {
	usecase       Usecase
	factory       func(*gin.Context) Completion
	audit         func(*gin.Context, int, string, map[string]any)
	externalError func(*gin.Context, error)
	writeError    func(*gin.Context, error)
}

func NewHTTP(usecase Usecase, factory func(*gin.Context) Completion, audit func(*gin.Context, int, string, map[string]any), externalError func(*gin.Context, error), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{usecase: usecase, factory: factory, audit: audit, externalError: externalError, writeError: writeError}
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
	result, err := h.usecase.Execute(op, op, op, op)
	if err != nil {
		bindingSucceeded, warning = result.BindingSucceeded, result.NotificationWarning
		var external *domain.ProviderError
		var reported *domain.ProviderMessageError
		switch {
		case errors.As(err, &external):
			h.externalError(c, external.Cause)
		case errors.As(err, &reported):
			c.JSON(http.StatusOK, gin.H{"success": false, "message": reported.Message})
		case errors.Is(err, domain.ErrProviderUnknown):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthUnknownProvider)})
		case errors.Is(err, domain.ErrOAuthStateInvalid):
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": i18n.T(c, i18n.MsgOAuthStateInvalid)})
		case errors.Is(err, domain.ErrInvalidInput):
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		case errors.Is(err, domain.ErrProviderDisabled):
			common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, map[string]any{"Provider": op.ProviderDisplayName()})
		case errors.Is(err, domain.ErrAlreadyBound):
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, map[string]any{"Provider": op.ProviderDisplayName()})
		case errors.Is(err, domain.ErrEmailTaken):
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		case errors.Is(err, domain.ErrAccountDeleted):
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case errors.Is(err, domain.ErrRegistrationDisabled):
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		case errors.Is(err, domain.ErrAccountBanned):
			common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		default:
			h.writeError(c, err)
		}
		return
	}
	switch result.Outcome {
	case OutcomeBound:
		common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, gin.H{"action": "bind", "notification_warning": result.NotificationWarning})
		bindingSucceeded, warning = result.BindingSucceeded, result.NotificationWarning
	case OutcomeLogin:
		op.CompleteLogin(c)
	case OutcomeVerified:
		proof := op.Proof()
		h.audit(c, op.ProofUserID(), "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope, "provider": c.Param("provider")})
		common.ApiSuccess(c, proof)
	}
}
