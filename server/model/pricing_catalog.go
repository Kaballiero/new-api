package model

import (
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

type CatalogPrice = pricingdomain.CatalogPrice
type CatalogOffer = pricingdomain.CatalogOffer

func SelectCatalogOffer(offers []CatalogOffer) *CatalogOffer {
	return pricingdomain.SelectCatalogOffer(offers)
}
