package finish_passkey_login

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	CompletePasskeyLogin(*gin.Context)
}
type HTTP struct {
	usecase    Usecase
	enabled    func() bool
	factory    func(c *gin.Context, request Request) (Completion, error)
	writeError func(*gin.Context, error)
}

// NewHTTP builds the handler; factory parses the WebAuthn assertion and
// returns its error.
func NewHTTP(usecase Usecase, enabled func() bool, factory func(c *gin.Context, request Request) (Completion, error), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{usecase: usecase, enabled: enabled, factory: factory, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyDisabled)
		return
	}
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyRequestInvalid)
		return
	}
	op, err := h.factory(c, request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if err := h.usecase.Execute(op); err != nil {
		h.writeError(c, err)
		return
	}
	c.Set("login_verification_method", "passkey")
	op.CompletePasskeyLogin(c)
}
