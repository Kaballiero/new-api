// Package transport holds the incoming HTTP code shared by account scenarios.
package transport

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/gin-gonic/gin"
)

// WriteError writes err as the HTTP error response of an account scenario.
// Errors without a domain meaning are written as {success:false} with HTTP 200
// and the text of the error.
func WriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrAccountNotFound):
		common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "user_not_found", i18n.MsgUserNotExists)
	case errors.Is(err, domain.ErrManageForbidden):
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionHigherLevel)
	case errors.Is(err, domain.ErrViewForbidden):
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionSameLevel)
	case errors.Is(err, domain.ErrCannotDisableRoot):
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "cannot_disable_root", i18n.MsgUserCannotDisableRootUser)
	case errors.Is(err, domain.ErrCannotDeleteRoot):
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "cannot_delete_root", i18n.MsgUserCannotDeleteRootUser)
	case errors.Is(err, domain.ErrAdminCannotPromote):
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "admin_cannot_promote", i18n.MsgUserAdminCannotPromote)
	case errors.Is(err, domain.ErrAlreadyAdmin):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "user_already_admin", i18n.MsgUserAlreadyAdmin)
	case errors.Is(err, domain.ErrCannotDemoteRoot):
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "cannot_demote_root", i18n.MsgUserCannotDemoteRootUser)
	case errors.Is(err, domain.ErrAlreadyCommon):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "user_already_common", i18n.MsgUserAlreadyCommon)
	case errors.Is(err, domain.ErrInvalidAction):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
	case errors.Is(err, domain.ErrUsernameTaken):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "username_already_taken", i18n.MsgUserExists)
	case errors.Is(err, domain.ErrQuotaChangeZero):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "quota_change_zero", i18n.MsgUserQuotaChangeZero)
	case errors.Is(err, domain.ErrQuotaAdjustmentInvalid):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
	case errors.Is(err, domain.ErrQuotaLimitExceeded):
		common.ApiError(c, err)
	case errors.Is(err, domain.ErrInternal) && errors.Is(err, domain.ErrAccountIDRequired):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgAccountIDRequired)
	case errors.Is(err, domain.ErrInternal):
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
	case errors.Is(err, domain.ErrGroupChangeFailed):
		common.ApiErrorStatus(c, http.StatusInternalServerError, err)
	case errors.Is(err, domain.ErrInvalidGroupChange):
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgInvalidParams)
	case errors.Is(err, domain.ErrGroupBatchTooLarge):
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgBatchTooMany, map[string]any{"Max": domain.MaxBulkGroupBatch})
	case errors.Is(err, domain.ErrGroupDoesNotExist):
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgGroupNotExists, groupParams(err))
	case errors.Is(err, domain.ErrGroupUsersNotFound):
		common.ApiErrorI18nStatus(c, http.StatusNotFound, i18n.MsgUserNotExists)
	case errors.Is(err, domain.ErrGroupChangeForbidden):
		common.ApiErrorI18nStatus(c, http.StatusForbidden, i18n.MsgUserNoPermissionHigherLevel)
	case errors.Is(err, domain.ErrRoleChangeNotAllowed), errors.Is(err, domain.ErrProfileInvalid):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case errors.Is(err, domain.ErrProfileInputFailed):
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
	case errors.Is(err, domain.ErrCreateForbidden):
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
	case errors.Is(err, domain.ErrBindingForbidden):
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
	case errors.Is(err, domain.ErrCheckinDisabled):
		common.ApiErrorI18n(c, i18n.MsgAccountsCheckinDisabled)
	case errors.Is(err, domain.ErrAlreadyCheckedIn):
		common.ApiErrorI18n(c, i18n.MsgAccountsCheckinAlreadyToday)
	case errors.Is(err, domain.ErrCheckinFailed):
		common.ApiErrorI18n(c, i18n.MsgAccountsCheckinFailed)
	case errors.Is(err, domain.ErrCheckinQuotaFailed):
		common.ApiErrorI18n(c, i18n.MsgAccountsCheckinQuotaFailed)
	case errors.Is(err, domain.ErrAffiliateTransferFailed):
		common.ApiErrorI18n(c, i18n.MsgUserTransferFailed, map[string]any{"Error": transferReason(c, err)})
	case errors.Is(err, domain.ErrSettingsSaveFailed):
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
	case errors.Is(err, domain.ErrNotifyTypeInvalid):
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
	case errors.Is(err, domain.ErrQuotaThresholdNotPositive):
		common.ApiErrorI18n(c, i18n.MsgQuotaThresholdGtZero)
	case errors.Is(err, domain.ErrWebhookURLEmpty):
		common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
	case errors.Is(err, domain.ErrWebhookURLInvalid):
		common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
	case errors.Is(err, domain.ErrNotificationEmailInvalid):
		common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
	case errors.Is(err, domain.ErrBarkURLEmpty):
		common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
	case errors.Is(err, domain.ErrBarkURLInvalid):
		common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
	case errors.Is(err, domain.ErrNotificationURLNotHTTP):
		common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
	case errors.Is(err, domain.ErrGotifyURLEmpty):
		common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
	case errors.Is(err, domain.ErrGotifyTokenEmpty):
		common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
	case errors.Is(err, domain.ErrGotifyURLInvalid):
		common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
	case errors.Is(err, domain.ErrAccountIDRequired):
		common.ApiErrorI18n(c, i18n.MsgAccountIDRequired)
	default:
		common.ApiError(c, err)
	}
}

func groupParams(err error) map[string]any {
	var group *domain.GroupError
	if !errors.As(err, &group) {
		return nil
	}
	return map[string]any{"Group": group.Group}
}

// transferReason localizes the reason embedded in a rejected affiliate
// transfer; other reasons keep the text of the cause.
func transferReason(c *gin.Context, err error) string {
	var minimum *domain.AmountError
	switch {
	case errors.As(err, &minimum) && errors.Is(minimum.Err, domain.ErrTransferBelowMinimum):
		return common.TranslateMessage(c, i18n.MsgAccountsTransferQuotaMinimum, map[string]any{"Min": minimum.Amount})
	case errors.Is(err, domain.ErrInviteQuotaInsufficient):
		return common.TranslateMessage(c, i18n.MsgAccountsInviteQuotaInsufficient)
	}
	return err.Error()
}
