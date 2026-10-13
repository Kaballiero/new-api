package model

import (
	_ "embed"
	"github.com/QuantumNous/new-api/common"
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

//go:embed pricing_catalog.json
var pricingCatalogJSON []byte

type catalogSource struct {
	Source    string         `json:"source"`
	FetchedAt string         `json:"fetched_at"`
	Offers    []CatalogOffer `json:"offers"`
}

var pricingCatalog = func() map[string]catalogSource {
	var snapshot struct {
		Models map[string]catalogSource `json:"models"`
	}
	if err := common.Unmarshal(pricingCatalogJSON, &snapshot); err != nil {
		panic(err)
	}
	return snapshot.Models
}()
