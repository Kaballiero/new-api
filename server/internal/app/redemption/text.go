package redemption

import (
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// receipts appends the legacy top-up log after commit, outside the redemption transaction.
type receipts struct{}

// quotaText renders a localized suffix and preserves custom currency symbols.
func quotaText(quota int, lang string) string {
	key := i18n.MsgRedemptionQuotaAmount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		key = i18n.MsgRedemptionQuotaTokens
	}
	amount := logger.FormatQuota(quota)
	if lang != i18n.LangZhCN && lang != i18n.LangZhTW &&
		operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeCustom {
		// The legacy formatter uses the fullwidth dollar for Chinese output.
		const asciiDollar = "$"
		amount = strings.ReplaceAll(amount, "\uff04", asciiDollar)
	}
	return i18n.Translate(lang, key, map[string]any{"Amount": amount})
}

func (receipts) Record(user int, code domain.Code) {
	lang := receiptLanguage(user)
	content := i18n.Translate(lang, i18n.MsgRedemptionCreditLog,
		map[string]any{"Quota": quotaText(code.Quota, lang), "ID": code.ID})
	model.RecordLog(user, model.LogTypeTopup, content)
}

// Receipt language comes from the beneficiary; both Chinese variants retain the legacy persisted bytes.
func receiptLanguage(user int) string {
	lang := i18n.ParseAcceptLanguage(model.GetUserLanguage(user))
	switch lang {
	case i18n.LangZhCN, i18n.LangZhTW:
		return i18n.LangZhCN
	case i18n.LangRu:
		return i18n.LangRu
	default:
		return i18n.DefaultLang
	}
}
