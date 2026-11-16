package pricing

import (
	"fmt"
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
// catalog revision changed more than domain.PriceChangeFactor times in lang.
// At most 12 changes are listed; a price that was free changed infinitely.
func CatalogPriceChangesNotification(lang string, changes []domain.CatalogPriceChange) (subject, content string) {
	shown := min(len(changes), 12)
	var builder strings.Builder
	builder.WriteString(i18n.Translate(lang, i18n.MsgPricingCatalogPriceChanges, map[string]any{"Factor": domain.PriceChangeFactor, "Shown": shown, "Total": len(changes)}))
	for _, change := range changes[:shown] {
		factor := "∞"
		if change.OldAmountRub > 0 {
			factor = fmt.Sprintf("%.2f", change.NewAmountRub/change.OldAmountRub)
		}
		builder.WriteString(fmt.Sprintf("\n- %s, %s: %.2f → %.2f ₽ / %s (×%s)", change.ModelID, change.Component, change.OldAmountRub, change.NewAmountRub, change.Unit, factor))
	}
	return i18n.Translate(lang, i18n.MsgPricingCatalogPriceChangesSubject, map[string]any{"Factor": domain.PriceChangeFactor}), builder.String()
}
