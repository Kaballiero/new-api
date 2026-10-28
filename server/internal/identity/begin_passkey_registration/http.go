package begin_passkey_registration

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Challenge() dto.PasskeyChallenge
}
type CompletionFactory func(authorization *domain.FlowAuthorization) Completion

type HTTP struct {
	usecase    Usecase
	enabled    func() bool
	user       func(*gin.Context) (domain.PasskeyAccount, CompletionFactory, error)
	authorize  func(*gin.Context) *domain.FlowAuthorization
	writeError func(*gin.Context, error)
}

// NewHTTP builds the handler; authorize writes its own response and returns
// nil when the security proof is missing or rejected.
func NewHTTP(usecase Usecase, enabled func() bool, user func(*gin.Context) (domain.PasskeyAccount, CompletionFactory, error), authorize func(*gin.Context) *domain.FlowAuthorization, writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{usecase: usecase, enabled: enabled, user: user, authorize: authorize, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityPasskeyDisabled)
		return
	}
	_, factory, err := h.user(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	authorization := h.authorize(c)
	if authorization == nil {
		return
	}
	op := factory(authorization)
	if err := h.usecase.Execute(op); err != nil {
		h.writeError(c, err)
		return
	}
	result := op.Challenge()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"options": result.Options, "flow_token": result.FlowToken, "expires_at": result.ExpiresAt}})
}
