// Package transport holds the incoming HTTP code shared by API key scenarios.
package transport

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/gin-gonic/gin"
)

// WriteError writes err as the HTTP error response of an API key scenario.
// Errors without a domain meaning are written as {success:false} with HTTP 200
// and the text of the error.
func WriteError(c *gin.Context, err error) {
	// Specific errors come before ErrInternal: a storage failure such as
	// ErrKeyIDRequired arrives wrapped in domain.Internal.
	switch {
	case errors.Is(err, domain.ErrKeyNotFound):
		common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "token_not_found", i18n.MsgTokenNotExists)
	case errors.Is(err, domain.ErrKeyIDRequired):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenIDRequired)
	case errors.Is(err, domain.ErrSearchConsecutiveWildcards):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenSearchConsecutiveWildcards)
	case errors.Is(err, domain.ErrSearchTooManyWildcards):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenSearchTooManyWildcards)
	case errors.Is(err, domain.ErrSearchKeywordTooShort):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenSearchKeywordTooShort)
	case errors.Is(err, domain.ErrSearchCountFailed):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenSearchCountFailed)
	case errors.Is(err, domain.ErrFuzzySearchLimit):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenFuzzySearchLimit)
	case errors.Is(err, domain.ErrSearchFailed):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "internal_error", i18n.MsgTokenSearchFailed)
	case errors.Is(err, domain.ErrInternal):
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
	case errors.Is(err, domain.ErrUsageUnavailable):
		common.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
	case errors.Is(err, domain.ErrNameTooLong):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "token_name_too_long", i18n.MsgTokenNameTooLong)
	case errors.Is(err, domain.ErrQuotaNegative):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "token_quota_negative", i18n.MsgTokenQuotaNegative)
	case errors.Is(err, domain.ErrQuotaExceedMax):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "token_quota_exceed_max", i18n.MsgTokenQuotaExceedMax, limitParams(err))
	case errors.Is(err, domain.ErrKeyCountLimit):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "token_count_limit", i18n.MsgTokenCountLimit, limitParams(err))
	case errors.Is(err, domain.ErrKeyGenerate):
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "token_generate_failed", i18n.MsgTokenGenerateFailed)
	case errors.Is(err, domain.ErrExpiredCannotEnable):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "token_expired_cannot_enable", i18n.MsgTokenExpiredCannotEnable)
	case errors.Is(err, domain.ErrExhaustedCannotEnable):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "token_exhausted_cannot_enable", i18n.MsgTokenExhaustedCannotEable)
	case errors.Is(err, domain.ErrAutoGroupsTooMany):
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsTooMany, limitParams(err))
	case errors.Is(err, domain.ErrAutoGroupDuplicate):
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsDuplicate, groupParams(err))
	case errors.Is(err, domain.ErrAutoGroupInvalid):
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsInvalid, groupParams(err))
	case errors.Is(err, domain.ErrBatchTooLarge):
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, limitParams(err))
	case errors.Is(err, domain.ErrBatchEmpty):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	default:
		common.ApiError(c, err)
	}
}

func limitParams(err error) map[string]any {
	var limit *domain.LimitError
	if !errors.As(err, &limit) {
		return nil
	}
	return map[string]any{"Max": limit.Max}
}

func groupParams(err error) map[string]any {
	var group *domain.GroupError
	if !errors.As(err, &group) {
		return nil
	}
	return map[string]any{"Group": group.Group}
}
