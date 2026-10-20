package model

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

type CatalogPrice = jsplugin.CatalogPrice
type CatalogOffer = jsplugin.CatalogOffer

func SelectCatalogOffer(offers []CatalogOffer) *CatalogOffer {
	return pricingdomain.SelectCatalogOffer(offers)
}
