package transport

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/i18n"
	keydomain "github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
)

// Translate preserves the historical Simplified bytes for both Chinese locales,
// including existing API-key search keys whose generic zh-TW text differs.
func Translate(c *gin.Context, key string, params ...map[string]any) string {
	lang := RequestLanguage(c)
	if lang == i18n.LangZhTW {
		lang = i18n.LangZhCN
	}
	return i18n.Translate(lang, key, params...)
}

// Message localizes recognized error identities. Unclassified storage errors
// retain their exact message, even when it resembles a localized domain error.
func Message(c *gin.Context, err error) string {
	var period *domain.InvalidRankingPeriod
	if errors.As(err, &period) {
		return Translate(c, i18n.MsgUsageRankingPeriod, map[string]any{"Period": period.Period})
	}
	var key string
	switch {
	case errors.Is(err, domain.ErrLogQuery):
		key = i18n.MsgUsageQueryFailed
	case errors.Is(err, domain.ErrLogStatsQuery):
		key = i18n.MsgUsageStatsFailed
	case errors.Is(err, domain.ErrQuotaSpan):
		key = i18n.MsgUsageSpanExceeded
	case errors.Is(err, domain.ErrUsageInvalidStart):
		key = i18n.MsgUsageInvalidStart
	case errors.Is(err, domain.ErrUsageInvalidEnd):
		key = i18n.MsgUsageInvalidEnd
	case errors.Is(err, domain.ErrUsageInvalidRange):
		key = i18n.MsgUsageInvalidRange
	case errors.Is(err, domain.ErrUsageInvalidTokenID):
		key = i18n.MsgUsageInvalidTokenID
	case errors.Is(err, domain.ErrPositiveQuotaTimes):
		key = i18n.MsgUsagePositiveTime
	case errors.Is(err, domain.ErrInvalidQuotaGranularity):
		key = i18n.MsgUsageGranularity
	case errors.Is(err, domain.ErrQuotaMonthSpanExceeded):
		key = i18n.MsgUsageMonthLimit
	case errors.Is(err, domain.ErrUsageAggregateRequestSize):
		key = i18n.MsgUsageAggregateRequestSize
	case errors.Is(err, domain.ErrUsageAggregateJSON):
		key = i18n.MsgUsageAggregateJSON
	case errors.Is(err, domain.ErrAggregateUserNotFound):
		key = i18n.MsgUsageAggregateNotFound
	case errors.Is(err, domain.ErrAggregateTooLarge):
		key = i18n.MsgUsageAggregateResponseSize
	case errors.Is(err, domain.ErrAggregateOverflow):
		key = i18n.MsgUsageAggregateOverflow
	case errors.Is(err, domain.ErrUsageAggregateUserCount):
		key = i18n.MsgUsageAggregateUserCount
	case errors.Is(err, domain.ErrUsageAggregateUserIDs):
		key = i18n.MsgUsageAggregateUserIDs
	case errors.Is(err, domain.ErrUsageAggregateTimes):
		key = i18n.MsgUsageAggregateTimes
	case errors.Is(err, domain.ErrUsageAggregateGranularity):
		key = i18n.MsgUsageAggregateGranularity
	case errors.Is(err, domain.ErrUsageAggregateMonths):
		key = i18n.MsgUsageAggregateMonths
	case errors.Is(err, domain.ErrUsageAggregateGroup):
		key = i18n.MsgUsageAggregateGroup
	case errors.Is(err, keydomain.ErrSearchConsecutiveWildcards):
		key = i18n.MsgTokenSearchConsecutiveWildcards
	case errors.Is(err, keydomain.ErrSearchTooManyWildcards):
		key = i18n.MsgTokenSearchTooManyWildcards
	case errors.Is(err, keydomain.ErrSearchKeywordTooShort):
		key = i18n.MsgTokenSearchKeywordTooShort
	}
	if key != "" {
		return Translate(c, key)
	}
	return err.Error()
}

// RequestLanguage supports direct handler boundaries without an HTTP request.
// Real requests retain the existing user-setting/header precedence.
func RequestLanguage(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return i18n.DefaultLang
	}
	return i18n.GetLangFromContext(c)
}
