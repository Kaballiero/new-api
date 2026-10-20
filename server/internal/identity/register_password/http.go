package register_password

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
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
	result := NewUsecase(h.operation).Execute(user)
	if result.Cause != nil {
		common.ApiError(c, result.Cause)
		return
	}
	if result.MessageKey != "" {
		keys := map[string]string{"invalid_params": i18n.MsgInvalidParams, "input_invalid": i18n.MsgUserInputInvalid, "email_required": i18n.MsgUserEmailVerificationRequired, "code_invalid": i18n.MsgUserVerificationCodeError, "email_taken": i18n.MsgUserEmailAlreadyTaken, "database": i18n.MsgDatabaseError, "exists": i18n.MsgUserExists, "register_failed": i18n.MsgUserRegisterFailed, "token_failed": i18n.MsgUserDefaultTokenFailed, "token_insert_failed": i18n.MsgCreateDefaultTokenErr}
		if result.Params != nil {
			common.ApiErrorI18n(c, keys[result.MessageKey], result.Params)
		} else {
			common.ApiErrorI18n(c, keys[result.MessageKey])
		}
		if result.LogAfter != "" {
			common.SysLog(result.LogAfter)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
