package finish_passkey_verification

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Proof() *dto.SecurityProof
}
type HTTP struct {
	enabled    func() bool
	user       func(*gin.Context) (*model.User, error)
	factory    func(c *gin.Context, user *model.User, request Request) (Completion, error)
	audit      func(*gin.Context, int, string, map[string]any)
	writeError func(*gin.Context, error)
}

// NewHTTP builds the handler; factory parses the WebAuthn assertion and
// returns its error.
func NewHTTP(enabled func() bool, user func(*gin.Context) (*model.User, error), factory func(c *gin.Context, user *model.User, request Request) (Completion, error), audit func(*gin.Context, int, string, map[string]any), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{enabled: enabled, user: user, factory: factory, audit: audit, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyDisabled)
		return
	}
	user, err := h.user(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.FlowToken == "" || len(request.Credential) == 0 {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyRequestInvalid)
		return
	}
	op, err := h.factory(c, user, request)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if err := NewUsecase(op).Execute(); err != nil {
		h.writeError(c, err)
		return
	}
	proof := op.Proof()
	h.audit(c, user.Id, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}
