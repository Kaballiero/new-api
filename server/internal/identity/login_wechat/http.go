package login_wechat

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Enabled() bool
	AuthenticatedUser() *model.User
}
type HTTP struct {
	factory  func() Completion
	complete func(*model.User, *gin.Context)
}

func NewHTTP(factory func() Completion, complete func(*model.User, *gin.Context)) *HTTP {
	return &HTTP{factory: factory, complete: complete}
}
func (h *HTTP) Handle(c *gin.Context) {
	op := h.factory()
	if !op.Enabled() {
		common.ApiErrorI18n(c, i18n.MsgIdentityWeChatDisabled)
		return
	}
	err := NewUsecase(op).Execute(Input{Code: c.Query("code"), Lang: i18n.GetLangFromContext(c)})
	message := ""
	switch {
	case err == nil:
		h.complete(op.AuthenticatedUser(), c)
		return
	case errors.Is(err, domain.ErrAccountDeleted):
		message = i18n.T(c, i18n.MsgIdentityWeChatAccountDeleted)
	case errors.Is(err, domain.ErrRegistrationDisabled):
		message = i18n.T(c, i18n.MsgIdentityWeChatRegistrationDisabled)
	case errors.Is(err, domain.ErrAccountBanned):
		message = i18n.T(c, i18n.MsgIdentityWeChatAccountBanned)
	case errors.Is(err, domain.ErrWeChatCodeMissing):
		message = i18n.T(c, i18n.MsgIdentityWeChatCodeMissing)
	case errors.Is(err, domain.ErrWeChatCodeInvalid):
		message = i18n.T(c, i18n.MsgIdentityWeChatCodeInvalid)
	default:
		message = err.Error()
	}
	c.JSON(http.StatusOK, gin.H{"message": message, "success": false})
}
