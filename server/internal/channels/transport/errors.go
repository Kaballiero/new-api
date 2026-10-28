// Package transport holds the incoming HTTP code shared by channel scenarios.
package transport

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/gin-gonic/gin"
)

// WriteError writes err as the HTTP error response of a channel scenario.
// Errors without their own status are written as {success:false} with HTTP 200
// and the message from Message.
func WriteError(c *gin.Context, err error) {
	// Classified errors come first: their causes may be other domain errors.
	switch {
	case errors.Is(err, domain.ErrValidation):
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "channel_validation_failed", Message(c, errors.Unwrap(err)))
	case errors.Is(err, domain.ErrRejected):
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", Message(c, errors.Unwrap(err)))
	case errors.Is(err, domain.ErrModelFetchFailed):
		common.ApiErrorI18nStatusCode(c, http.StatusBadGateway, "fetch_models_failed", i18n.MsgChannelModelFetchResponseFailed, map[string]any{"Error": Message(c, errors.Unwrap(err))})
	case errors.Is(err, domain.ErrOllamaVersionFailed):
		common.ApiErrorI18n(c, i18n.MsgChannelOllamaVersionResponseFailed, map[string]any{"Error": err.Error()})
	case errors.Is(err, domain.ErrInternal):
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
	case errors.Is(err, domain.ErrChannelNotFound):
		common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
	case errors.Is(err, domain.ErrAddModeUnsupported):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "mode_unsupported", i18n.MsgChannelAddModeUnsupported)
	case errors.Is(err, domain.ErrOllamaInputRequired), errors.Is(err, domain.ErrNotOllamaChannel):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, domain.ErrOllamaNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, domain.ErrOllamaPullFailed), errors.Is(err, domain.ErrOllamaDeleteFailed):
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
	default:
		common.ApiErrorMsg(c, Message(c, err))
	}
}

// messages lists the domain errors whose client text is localized. Errors
// that are not listed reach clients with their own (English) text.
var messages = []struct {
	err error
	key string
}{
	{domain.ErrStatusNotEditable, i18n.MsgInvalidParams},
	{domain.ErrSensitiveWriteDenied, i18n.MsgAuthInsufficientPrivilege},
	{domain.ErrVertexRegionRequired, i18n.MsgChannelVertexRegionRequired},
	{domain.ErrVertexRegionInvalid, i18n.MsgChannelVertexRegionInvalid},
	{domain.ErrVertexRegionDefaultMissing, i18n.MsgChannelVertexRegionDefaultMissing},
	{domain.ErrVertexKeysEmpty, i18n.MsgChannelVertexKeysEmpty},
	{domain.ErrTagRequired, i18n.MsgChannelTagRequired},
	{domain.ErrParamOverrideInvalid, i18n.MsgChannelParamOverrideInvalid},
	{domain.ErrHeaderOverrideInvalid, i18n.MsgChannelHeaderOverrideInvalid},
	{domain.ErrCopySourceUnavailable, i18n.MsgChannelCopySourceFailed},
	{domain.ErrCopyFailed, i18n.MsgChannelCopyFailed},
	{domain.ErrListTagsFailed, i18n.MsgChannelTagsResponseFailed},
	{domain.ErrCountTagsFailed, i18n.MsgChannelCountTagsFailed},
	{domain.ErrListTagChannelsFailed, i18n.MsgChannelGetTagChannelsFailed},
	{domain.ErrCountChannelsFailed, i18n.MsgChannelCountFailed},
	{domain.ErrListChannelsFailed, i18n.MsgChannelListResponseFailed},
	{domain.ErrCountTypesFailed, i18n.MsgChannelCountTypesFailed},
	{domain.ErrMultiKeyChannelNotFound, i18n.MsgChannelMultiKeyNotFound},
	{domain.ErrNotMultiKey, i18n.MsgChannelNotMultiKey},
	{domain.ErrDisableKeyIndexRequired, i18n.MsgChannelDisableKeyIndexRequired},
	{domain.ErrEnableKeyIndexRequired, i18n.MsgChannelEnableKeyIndexRequired},
	{domain.ErrDeleteKeyIndexRequired, i18n.MsgChannelDeleteKeyIndexRequired},
	{domain.ErrKeyIndexOutOfRange, i18n.MsgChannelKeyIndexOutOfRange},
	{domain.ErrNoKeysToDisable, i18n.MsgChannelNoKeysToDisable},
	{domain.ErrLastKeyNotDeletable, i18n.MsgChannelLastKeyNotDeletable},
	{domain.ErrNoAutoDisabledKeys, i18n.MsgChannelNoAutoDisabledKeys},
	{domain.ErrMultiKeyActionUnsupported, i18n.MsgChannelMultiKeyActionUnsupported},
	{domain.ErrBalanceMultiKey, i18n.MsgChannelBalanceMultiKey},
	{domain.ErrBalanceNotImplemented, i18n.MsgChannelBalanceNotImplemented},
	{domain.ErrCodexCredentialInvalid, i18n.MsgChannelCodexCredentialInvalid},
	{domain.ErrCodexUsageFailed, i18n.MsgChannelCodexUsageFailed},
	{domain.ErrCodexResetCreditsFailed, i18n.MsgChannelCodexResetCreditsFailed},
	{domain.ErrCodexResetFailed, i18n.MsgChannelCodexResetFailed},
}

// detailMessages lists the localized domain errors that carry a detail.
var detailMessages = map[error]string{
	domain.ErrChannelSettingInvalid: i18n.MsgChannelSettingInvalid,
	domain.ErrChannelKeyUnavailable: i18n.MsgChannelKeyUnavailable,
	domain.ErrModelNameTooLong:      i18n.MsgChannelModelNameTooLong,
	domain.ErrVertexKeysInvalid:     i18n.MsgChannelVertexKeysInvalid,
	domain.ErrVertexKeyEncode:       i18n.MsgChannelVertexKeyEncodeFailed,
	domain.ErrAppendKeysInvalid:     i18n.MsgChannelAppendKeysInvalid,
}

// Message returns the client text of err in the language of the request.
func Message(c *gin.Context, err error) string {
	var detailed *domain.DetailError
	if errors.As(err, &detailed) {
		if key, ok := detailMessages[detailed.Err]; ok {
			detail := detailed.Detail
			if detailed.Cause != nil {
				detail = Message(c, detailed.Cause)
			}
			return i18n.T(c, key, map[string]any{"Detail": detail})
		}
	}
	for _, message := range messages {
		if errors.Is(err, message.err) {
			return i18n.T(c, message.key)
		}
	}
	return err.Error()
}
