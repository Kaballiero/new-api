package register_password

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	enabled, passwordEnabled func() bool
	operation                Operation
}

func NewHTTP(enabled, passwordEnabled func() bool, op Operation) *HTTP {
	return &HTTP{enabled: enabled, passwordEnabled: passwordEnabled, operation: op}
}
func (h *HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !h.passwordEnabled() {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordRegisterDisabled)
		return
	}
	var user User
	if err := common.DecodeJson(c.Request.Body, &user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	err := NewUsecase(h.operation).Execute(Input{User: user, Lang: i18n.GetLangFromContext(c)})
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
		return
	}
	var input *domain.InputError
	switch {
	case errors.As(err, &input):
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": input.Detail})
	case errors.Is(err, domain.ErrInvalidInput):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case errors.Is(err, domain.ErrEmailVerificationRequired):
		common.ApiErrorI18n(c, i18n.MsgUserEmailVerificationRequired)
	case errors.Is(err, domain.ErrVerificationCodeInvalid):
		common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
	case errors.Is(err, domain.ErrEmailTaken):
		common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
	case errors.Is(err, domain.ErrStorageUnavailable):
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
	case errors.Is(err, domain.ErrUserExists):
		common.ApiErrorI18n(c, i18n.MsgUserExists)
	case errors.Is(err, domain.ErrRegisterFailed):
		common.ApiErrorI18n(c, i18n.MsgUserRegisterFailed)
	case errors.Is(err, domain.ErrDefaultTokenFailed):
		common.ApiErrorI18n(c, i18n.MsgUserDefaultTokenFailed)
	case errors.Is(err, domain.ErrDefaultTokenInsertFailed):
		common.ApiErrorI18n(c, i18n.MsgCreateDefaultTokenErr)
	default:
		common.ApiError(c, err)
		return
	}
	var logged *domain.LoggedError
	if errors.As(err, &logged) {
		common.SysLog(logged.Log)
	}
}
