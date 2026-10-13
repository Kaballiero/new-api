package model

import (
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"math"
	"slices"
)

type CatalogPrice = jsplugin.CatalogPrice
type CatalogOffer = jsplugin.CatalogOffer

func (pricing Pricing) CatalogOffersForGroup(group string) []CatalogOffer {
	result := slices.Clone(pricing.CatalogOffersByGroup["all"])
	return append(result, pricing.CatalogOffersByGroup[group]...)
}

func SelectCatalogOffer(offers []CatalogOffer) *CatalogOffer {
	var selected *CatalogOffer
	var selectedAmount float64
	var unit string
	for index := range offers {
		offer := &offers[index]
		for _, price := range offer.Prices {
			if price.Component != offer.MainComponent || price.AmountUSD == nil || *price.AmountUSD < 0 || math.IsNaN(*price.AmountUSD) || math.IsInf(*price.AmountUSD, 0) {
				continue
			}
			if unit != "" && unit != price.Unit {
				return nil
			}
			unit = price.Unit
			if selected == nil || *price.AmountUSD < selectedAmount || (*price.AmountUSD == selectedAmount && offer.ID < selected.ID) {
				selected, selectedAmount = offer, *price.AmountUSD
			}
		}
	}
	return selected
}
