package redemption

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// receipts appends the legacy top-up log after commit, outside the redemption transaction.
type receipts struct{}

// quotaText preserves the legacy display currency and Chinese suffix without duplicating quota arithmetic.
func quotaText(quota int, lang string) string {
	key := i18n.MsgRedemptionQuotaAmount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		key = i18n.MsgRedemptionQuotaTokens
	}
	return i18n.Translate(lang, key, map[string]any{"Amount": logger.FormatQuota(quota)})
}

func (receipts) Record(user int, code domain.Code) {
	content := i18n.Translate(i18n.LangZhCN, i18n.MsgRedemptionCreditLog,
		map[string]any{"Quota": quotaText(code.Quota, i18n.LangZhCN), "ID": code.ID})
	model.RecordLog(user, model.LogTypeTopup, content)
}
