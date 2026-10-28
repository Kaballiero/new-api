package start_oauth_flow

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	BindingAudit() (bool, int, bool)
	Response() map[string]any
}
type HTTP struct {
	factory    func(*gin.Context, *Request) Completion
	audit      func(*gin.Context, int, string, map[string]any)
	writeError func(*gin.Context, error)
}

func NewHTTP(factory func(*gin.Context, *Request) Completion, audit func(*gin.Context, int, string, map[string]any), writeError func(*gin.Context, error)) *HTTP {
	return &HTTP{factory: factory, audit: audit, writeError: writeError}
}
func (h *HTTP) Handle(c *gin.Context) {
	var request Request
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	op := h.factory(c, &request)
	defer func() {
		required, id, succeeded := op.BindingAudit()
		if required {
			h.audit(c, id, "user.binding_start", map[string]any{"provider": request.Provider, "success": succeeded})
		}
	}()
	err := NewUsecase(op).Execute(&request)
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	case errors.Is(err, domain.ErrBindingSessionMissing):
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": i18n.T(c, i18n.MsgIdentityBindingLoginRequired)})
		return
	case errors.Is(err, domain.ErrStopped):
		return
	case err != nil:
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": op.Response()})
}
