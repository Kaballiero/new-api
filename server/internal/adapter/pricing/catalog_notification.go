package pricing

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// CatalogModelLossNotification phrases the rejection of an upstream catalog
// revision in lang. The lost share is rounded up, so a rejected share never
// reads as the allowed one. At most 12 lost models are listed.
func CatalogModelLossNotification(lang string, lossErr domain.CatalogModelLossError) (subject, content string) {
	shown := min(len(lossErr.Lost), 12)
	return i18n.Translate(lang, i18n.MsgPricingCatalogModelLossSubject), i18n.Translate(lang, i18n.MsgPricingCatalogModelLoss, map[string]any{
		"Revision":  lossErr.RemoteRevision,
		"Head":      lossErr.HeadRemoteRevision,
		"Lost":      len(lossErr.Lost),
		"Published": lossErr.Published,
		"Percent":   (len(lossErr.Lost)*100 + lossErr.Published - 1) / lossErr.Published,
		"Limit":     domain.MaxCatalogModelLossPercent,
		"Shown":     shown,
		"Models":    strings.Join(lossErr.Lost[:shown], ", "),
	})
}

// CatalogPriceChangesNotification phrases the prices an applied upstream
// catalog revision changed more than domain.PriceChangeFactor times in lang,
// with the revisions shortened to 12 characters. The amounts are roubles at
// usdRate roubles per USD, or USD when the rate is not valid. At most 12
// changes are listed; a price that was free changed infinitely.
func CatalogPriceChangesNotification(lang string, report domain.CatalogPriceChangeReport, usdRate float64, validRate bool) (subject, content string) {
	currency, rate, amounts := "USD", 1.0, i18n.Translate(lang, i18n.MsgPricingCatalogPriceChangesUSD)
	if validRate {
		currency, rate, amounts = "₽", usdRate, i18n.Translate(lang, i18n.MsgPricingCatalogPriceChangesRub, map[string]any{"Rate": significantDigits(usdRate)})
	}
	shown := min(len(report.Changes), 12)
	var builder strings.Builder
	builder.WriteString(i18n.Translate(lang, i18n.MsgPricingCatalogPriceChanges, map[string]any{
		"Head":     report.HeadRemoteRevision[:12],
		"Revision": report.RemoteRevision[:12],
		"Factor":   domain.PriceChangeFactor,
		"Currency": amounts,
		"Shown":    shown,
		"Total":    len(report.Changes),
	}))
	for _, change := range report.Changes[:shown] {
		factor := "∞"
		if change.OldAmountUSD > 0 {
			factor = significantDigits(change.NewAmountUSD / change.OldAmountUSD)
		}
		builder.WriteString(fmt.Sprintf("\n- %s, %s: %s → %s %s / %s (×%s)", change.ModelID, change.Component, significantDigits(change.OldAmountUSD*rate), significantDigits(change.NewAmountUSD*rate), currency, change.Unit, factor))
	}
	return i18n.Translate(lang, i18n.MsgPricingCatalogPriceChangesSubject, map[string]any{"Factor": domain.PriceChangeFactor}), builder.String()
}

// significantDigits writes an amount with 4 significant digits and a decimal
// point, without an exponent: a price per character stays readable.
func significantDigits(amount float64) string {
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(amount, 'g', 4, 64), 64)
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}
