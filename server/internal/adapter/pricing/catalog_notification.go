package pricing

import (
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// CatalogModelLossNotification phrases the rejection of an upstream catalog
// revision in lang. At most 12 lost models are listed.
func CatalogModelLossNotification(lang string, lossErr domain.CatalogModelLossError) (subject, content string) {
	shown := min(len(lossErr.Lost), 12)
	return i18n.Translate(lang, i18n.MsgPricingCatalogModelLossSubject), i18n.Translate(lang, i18n.MsgPricingCatalogModelLoss, map[string]any{
		"Revision":  lossErr.RemoteRevision,
		"Head":      lossErr.HeadRemoteRevision,
		"Lost":      len(lossErr.Lost),
		"Published": lossErr.Published,
		"Percent":   len(lossErr.Lost) * 100 / lossErr.Published,
		"Limit":     domain.MaxCatalogModelLossPercent,
		"Shown":     shown,
		"Models":    strings.Join(lossErr.Lost[:shown], ", "),
	})
}
