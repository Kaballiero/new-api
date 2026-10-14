package jsplugin

import (
	"fmt"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"math"
	"slices"
	"time"
)

type CatalogPrice struct {
	Component string   `json:"component"`
	Unit      string   `json:"unit"`
	AmountUSD *float64 `json:"amount_usd"`
}

type CatalogOffer struct {
	PublicAlternatives []CatalogOffer              `json:"display_alternatives,omitempty"`
	PublicConditions   []billingexpr.TierCondition `json:"display_conditions,omitempty"`
	PublicConditional  bool                        `json:"display_conditional,omitempty"`
	ID                 string                      `json:"id"`
	Operation          string                      `json:"operation"`
	Mode               string                      `json:"mode"`
	MainComponent      string                      `json:"main_component"`
	Prices             []CatalogPrice              `json:"prices"`
}

type PriceCatalog struct {
	FetchedAt string                    `json:"fetched_at"`
	Models    map[string][]CatalogOffer `json:"models"`
}

func (catalog *PriceCatalog) Validate(models []string) error {
	if catalog == nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339Nano, catalog.FetchedAt); err != nil {
		return fmt.Errorf("plugin price catalog fetched_at is invalid")
	}
	if len(catalog.Models) > 1024 {
		return fmt.Errorf("plugin price catalog is too large")
	}
	for model, offers := range catalog.Models {
		if !slices.Contains(models, model) || len(offers) > 128 {
			return fmt.Errorf("plugin price catalog model is undeclared or too large")
		}
		for _, offer := range offers {
			if offer.ID == "" || offer.Operation == "" || offer.MainComponent == "" || !slices.Contains([]string{"sync", "batch"}, offer.Mode) || len(offer.Prices) > 32 {
				return fmt.Errorf("plugin price catalog offer is invalid")
			}
			components := make(map[string]bool)
			for _, price := range offer.Prices {
				if price.Component == "" || components[price.Component] || !slices.Contains([]string{"million_tokens", "image", "megapixel", "second", "call", "megapixel_second", "search"}, price.Unit) {
					return fmt.Errorf("plugin price catalog component is invalid")
				}
				components[price.Component] = true
				if price.AmountUSD != nil && (*price.AmountUSD < 0 || math.IsNaN(*price.AmountUSD) || math.IsInf(*price.AmountUSD, 0)) {
					return fmt.Errorf("plugin price catalog amount is invalid")
				}
			}
		}
	}
	return nil
}

func (catalog *PriceCatalog) clone() *PriceCatalog {
	if catalog == nil {
		return nil
	}
	result := &PriceCatalog{FetchedAt: catalog.FetchedAt, Models: make(map[string][]CatalogOffer, len(catalog.Models))}
	for model, offers := range catalog.Models {
		copied := slices.Clone(offers)
		for index := range copied {
			copied[index].Prices = slices.Clone(copied[index].Prices)
			for priceIndex := range copied[index].Prices {
				if value := copied[index].Prices[priceIndex].AmountUSD; value != nil {
					amount := *value
					copied[index].Prices[priceIndex].AmountUSD = &amount
				}
			}
		}
		result.Models[model] = copied
	}
	return result
}
