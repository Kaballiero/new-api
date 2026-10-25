package bind_wechat

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

type Completion interface {
	Operation
	Succeeded() bool
	NotificationWarning() bool
}
type HTTP struct {
	identity   func(*gin.Context) (domain.AuthIdentity, bool)
	authorize  func(*gin.Context, []byte) bool
	factory    func(Input) Completion
	writeError func(*gin.Context, error)
	audit      func(*gin.Context, int, string, map[string]any)
}

// NewHTTP builds the handler; authorize writes its own response and returns
// false when the security proof for the binding context is missing or rejected.
func NewHTTP(identity func(*gin.Context) (domain.AuthIdentity, bool), authorize func(*gin.Context, []byte) bool, factory func(Input) Completion, writeError func(*gin.Context, error), audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{identity: identity, authorize: authorize, factory: factory, writeError: writeError, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		h.writeError(c, domain.ErrAuthTokenInvalid)
		return
	}
	var op Completion
	defer func() {
		succeeded, warning := false, false
		if op != nil {
			succeeded, warning = op.Succeeded(), op.NotificationWarning()
		}
		h.audit(c, identity.UserID, "user.binding_bind", map[string]any{"provider": "wechat", "success": succeeded, "notification_failed": warning})
	}()
	if !common.WeChatAuthEnabled {
		common.ApiErrorI18n(c, i18n.MsgIdentityWeChatDisabled)
		return
	}
	var req Request
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgIdentityRequestInvalid)
		return
	}
	code := strings.TrimSpace(req.Code)
	context, err := common.Marshal(domain.AccountBindingContext{Provider: "wechat", Code: code})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if !h.authorize(c, context) {
		return
	}
	input := Input{Identity: identity, Code: code}
	op = h.factory(input)
	err = NewUsecase(op).Execute(input)
	var provider *domain.ProviderError
	switch {
	case errors.As(err, &provider):
		message := provider.Error()
		switch {
		case errors.Is(err, domain.ErrWeChatCodeMissing):
			message = i18n.T(c, i18n.MsgIdentityWeChatCodeMissing)
		case errors.Is(err, domain.ErrWeChatCodeInvalid):
			message = i18n.T(c, i18n.MsgIdentityWeChatCodeInvalid)
		}
		c.JSON(http.StatusOK, gin.H{"message": message, "success": false})
		return
	case errors.Is(err, domain.ErrWeChatTaken):
		common.ApiErrorI18n(c, i18n.MsgIdentityWeChatAlreadyBound)
		return
	case err != nil:
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"notification_warning": op.NotificationWarning()}})
}
