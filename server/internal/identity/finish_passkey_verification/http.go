package finish_passkey_verification

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Completion interface {
	Operation
	Proof() *dto.SecurityProof
}
type CompletionFactory func(request Request) (Completion, error)

type HTTP struct {
	usecase    Usecase
	enabled    func() bool
	user       func(*gin.Context) (domain.PasskeyAccount, CompletionFactory, error)
	audit      func(*gin.Context, int, string, map[string]any)
	writeError func(*gin.Context, error)
}

// NewHTTP builds the handler; factory parses the WebAuthn assertion and
// returns its error.
func NewHTTP(usecase Usecase, enabled func() bool, user func(*gin.Context) (domain.PasskeyAccount, CompletionFactory, error), audit func(*gin.Context, int, string, map[string]any), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{usecase: usecase, enabled: enabled, user: user, audit: audit, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyDisabled)
		return
	}
	user, factory, err := h.user(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyRequestInvalid)
		return
	}
	op, err := factory(request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if err := h.usecase.Execute(op); err != nil {
		h.writeError(c, err)
		return
	}
	proof := op.Proof()
	h.audit(c, user.ID, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}
