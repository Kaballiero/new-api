package finish_passkey_login

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	LoginAccount() *model.User
}
type HTTP struct {
	enabled    func() bool
	factory    func(c *gin.Context, request Request) (Completion, error)
	complete   func(*gin.Context, *model.User, int64)
	writeError func(*gin.Context, error)
}

// NewHTTP builds the handler; factory parses the WebAuthn assertion and
// returns its error.
func NewHTTP(enabled func() bool, factory func(c *gin.Context, request Request) (Completion, error), complete func(*gin.Context, *model.User, int64), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{enabled: enabled, factory: factory, complete: complete, writeError: writeError}
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
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	user := op.LoginAccount()
	c.Set("login_verification_method", "passkey")
	h.complete(c, user, user.AuthVersion)
}
