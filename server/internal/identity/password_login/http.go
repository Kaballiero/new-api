package password_login

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	enabled   func() bool
	operation Operation
	complete  func(*gin.Context, Account)
}

func NewHTTP(enabled func() bool, op Operation, complete func(*gin.Context, Account)) *HTTP {
	return &HTTP{enabled: enabled, operation: op, complete: complete}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)
		return
	}
	var input LoginRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	account, err := NewUsecase(h.operation).Execute(input)
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case errors.Is(err, domain.ErrCredentialsInvalid):
		common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
	case errors.Is(err, domain.ErrStorageUnavailable):
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
	default:
		h.complete(c, account)
	}
}
