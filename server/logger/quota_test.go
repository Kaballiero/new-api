package logger_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
)

func TestQuotaDisplayPreservesLegacyAndEnglishCurrency(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	original, originalUnit, originalRate := *settings, common.QuotaPerUnit, operation_setting.USDExchangeRate
	t.Cleanup(func() {
		*settings = original
		common.QuotaPerUnit, operation_setting.USDExchangeRate = originalUnit, originalRate
	})
	common.QuotaPerUnit, operation_setting.USDExchangeRate = 500000, 7.3
	for _, tc := range []struct {
		name, display, symbol  string
		rate                   float64
		quota                  int
		legacy, english, plain string
	}{
		{name: "USD", display: "USD", quota: 500000, legacy: "＄1.000000 额度", english: "$1.000000 quota", plain: "＄1.000000"},
		{name: "unknown uses USD", display: "unknown", quota: 500000, legacy: "＄1.000000 额度", english: "$1.000000 quota", plain: "＄1.000000"},
		{name: "CNY", display: "CNY", quota: 500000, legacy: "¥7.300000 额度", english: "¥7.300000 quota", plain: "¥7.300000"},
		{name: "tokens", display: "TOKENS", quota: 500000, legacy: "500000 点额度", english: "500000 quota units", plain: "500000"},
		{name: "custom", display: "CUSTOM", symbol: "C$", rate: 2.5, quota: 500000, legacy: "C$2.500000 额度", english: "C$2.500000 quota", plain: "C$2.500000"},
		{name: "custom full-width symbol stays", display: "CUSTOM", symbol: "＄", rate: 2, quota: 500000, legacy: "＄2.000000 额度", english: "＄2.000000 quota", plain: "＄2.000000"},
		{name: "custom fallbacks", display: "CUSTOM", rate: -1, quota: 500000, legacy: "¤1.000000 额度", english: "¤1.000000 quota", plain: "¤1.000000"},
		{name: "USD zero", display: "USD", legacy: "＄0.000000 额度", english: "$0.000000 quota", plain: "＄0.000000"},
		{name: "USD negative", display: "USD", quota: -1, legacy: "＄-0.000002 额度", english: "$-0.000002 quota", plain: "＄-0.000002"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.QuotaDisplayType, settings.CustomCurrencySymbol, settings.CustomCurrencyExchangeRate = tc.display, tc.symbol, tc.rate
			assert.Equal(t, tc.legacy, logger.LogQuota(tc.quota))
			assert.Equal(t, tc.english, logger.LogQuotaEnglish(tc.quota))
			assert.Equal(t, tc.plain, logger.FormatQuota(tc.quota))
		})
	}
}
