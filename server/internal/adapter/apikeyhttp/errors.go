package apikeyhttp

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

func WriteFailure(c *gin.Context, f *domain.Failure) {
	switch f.Kind {
	case "missing":
		common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "token_not_found", i18n.MsgTokenNotExists)
	case "internal":
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", f.Cause)
	case "plain":
		common.ApiError(c, f.Cause)
	case "usage":
		common.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
	case "name":
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "token_name_too_long", i18n.MsgTokenNameTooLong, f.Params)
	case "quota_negative":
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "token_quota_negative", i18n.MsgTokenQuotaNegative, f.Params)
	case "quota_exceed":
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "token_quota_exceed_max", i18n.MsgTokenQuotaExceedMax, f.Params)
	case "count":
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "token_count_limit", i18n.MsgTokenCountLimit, f.Params)
	case "generate":
		common.ApiErrorI18nStatusCode(c, http.StatusInternalServerError, "token_generate_failed", i18n.MsgTokenGenerateFailed, f.Params)
	case "expired":
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "token_expired_cannot_enable", i18n.MsgTokenExpiredCannotEnable, f.Params)
	case "exhausted":
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "token_exhausted_cannot_enable", i18n.MsgTokenExhaustedCannotEable, f.Params)
	case "auto_too_many":
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsTooMany, f.Params)
	case "auto_duplicate":
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsDuplicate, f.Params)
	case "auto_invalid":
		common.ApiErrorI18n(c, i18n.MsgTokenAutoGroupsInvalid, f.Params)
	case "batch_many":
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, f.Params)
	case "invalid":
		common.ApiErrorI18n(c, i18n.MsgInvalidParams, f.Params)
	default:
		panic("unknown API key failure: " + f.Kind)
	}
}
