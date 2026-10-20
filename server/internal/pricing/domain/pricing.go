package domain

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"maps"
	"math"
	"slices"
	"strings"
)

type CatalogPrice = jsplugin.CatalogPrice
type CatalogOffer = jsplugin.CatalogOffer

const (
	BillingSourceConfiguredTariff                           = "configured_tariff"
	BillingSourceOpenRouterActualCost                       = "openrouter_actual_cost"
	PricingEndpointBatch              constant.EndpointType = "openai_batch"
	PricingEndpointSystemOne          constant.EndpointType = "pricing_systemone"
)

type Pricing struct {
	CatalogType                     string                                        `json:"-"`
	CatalogModality                 string                                        `json:"-"`
	DisplayName                     string                                        `json:"display_name,omitempty"`
	UpstreamCatalogRoutesByGroup    map[string][]UpstreamCatalogRoute             `json:"-"`
	PublicCatalogOffersByGroup      map[string][]CatalogOffer                     `json:"-"`
	ConsumerCanonicalModelsByGroup  map[string]string                             `json:"-"`
	ConfiguredPrices                map[string][][]billingexpr.DisplayPrice       `json:"-"`
	PriceConfigured                 bool                                          `json:"-"`
	CatalogOffersByGroup            map[string][]CatalogOffer                     `json:"-"`
	BillingSourcesByGroup           map[string]map[constant.EndpointType][]string `json:"billing_sources_by_group,omitempty"`
	IndicativeBillingExprByEndpoint map[constant.EndpointType]string              `json:"indicative_billing_expr_by_endpoint,omitempty"`
	ModelName                       string                                        `json:"model_name"`
	Description                     string                                        `json:"description,omitempty"`
	Icon                            string                                        `json:"icon,omitempty"`
	Tags                            string                                        `json:"tags,omitempty"`
	VendorID                        int                                           `json:"vendor_id,omitempty"`
	QuotaType                       int                                           `json:"quota_type"`
	ModelRatio                      float64                                       `json:"model_ratio"`
	ModelPrice                      float64                                       `json:"model_price"`
	OwnerBy                         string                                        `json:"owner_by"`
	CompletionRatio                 float64                                       `json:"completion_ratio"`
	CacheRatio                      *float64                                      `json:"cache_ratio,omitempty"`
	CreateCacheRatio                *float64                                      `json:"create_cache_ratio,omitempty"`
	ImageRatio                      *float64                                      `json:"image_ratio,omitempty"`
	AudioRatio                      *float64                                      `json:"audio_ratio,omitempty"`
	AudioCompletionRatio            *float64                                      `json:"audio_completion_ratio,omitempty"`
	EnableGroup                     []string                                      `json:"enable_groups"`
	SupportedEndpointTypes          []constant.EndpointType                       `json:"supported_endpoint_types"`
	BillingMode                     string                                        `json:"billing_mode,omitempty"`
	BillingExpr                     string                                        `json:"billing_expr,omitempty"`
	BillingUsageSchema              map[string]jsplugin.UsageFieldSchema          `json:"billing_usage_schema,omitempty"`
	BillingUsageExamples            []jsplugin.UsageExample                       `json:"billing_usage_examples,omitempty"`
	PricingVersion                  string                                        `json:"pricing_version,omitempty"`

	LegacyVideoPriceBasisByGroup map[string][]map[string]jsplugin.LegacyPriceBasis `json:"-"`
}
type PricingVendor struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
}
type UpstreamCatalogRoute struct {
	Operation    string
	Mode         string
	Endpoint     constant.EndpointType
	ModelID      string
	DocumentHash string
	Document     *catalogsync.Document
	// Overrides are private projection inputs, never consumer response fields.
	ChannelID          int     `json:"-"`
	ParamOverride      *string `json:"-"`
	ServiceTierEnabled bool    `json:"-"`
}

func (p Pricing) LegacyVideoPriceBasisForGroup(group string) (map[string]jsplugin.LegacyPriceBasis, bool) {
	var basis map[string]jsplugin.LegacyPriceBasis
	for _, sourceGroup := range []string{"all", group} {
		for _, routeBasis := range p.LegacyVideoPriceBasisByGroup[sourceGroup] {
			if len(routeBasis) == 0 || (basis != nil && !maps.Equal(basis, routeBasis)) {
				return nil, false
			}
			basis = routeBasis
		}
	}
	return maps.Clone(basis), len(basis) > 0
}

// BillingSourcesForGroup expands universal abilities without modifying the cached snapshot.
func (p Pricing) BillingSourcesForGroup(group string) map[constant.EndpointType][]string {
	sourcesByEndpoint := make(map[constant.EndpointType][]string)
	for _, sourceGroup := range []string{"all", group} {
		for endpoint, sources := range p.BillingSourcesByGroup[sourceGroup] {
			for _, source := range sources {
				if !slices.Contains(sourcesByEndpoint[endpoint], source) {
					sourcesByEndpoint[endpoint] = append(sourcesByEndpoint[endpoint], source)
				}
			}
		}
	}
	return sourcesByEndpoint
}
func (pricing Pricing) CatalogOffersForGroup(group string) []CatalogOffer {
	result := slices.Clone(pricing.CatalogOffersByGroup["all"])
	return append(result, pricing.CatalogOffersByGroup[group]...)
}
func (p Pricing) UpstreamCatalogRoutesForGroup(group string) []UpstreamCatalogRoute {
	return append(slices.Clone(p.UpstreamCatalogRoutesByGroup["all"]), p.UpstreamCatalogRoutesByGroup[group]...)
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

// Legacy normalization v2 kept the exact Batch identity and variant while Mode
// was nullable. Require both facts; a model-name suffix alone is not a tariff.
func CatalogOfferMode(offer catalogsync.Offer, upstream string) (string, bool) {
	if offer.Mode != nil {
		return *offer.Mode, *offer.Mode == "sync" || *offer.Mode == "batch"
	}
	var identity struct {
		ModelID string `json:"modelId"`
	}
	var variant struct {
		Variant   string `json:"variant"`
		ModelSlug string `json:"model_variant_slug"`
	}
	if len(offer.Identity) > 0 && common.Unmarshal(offer.Identity, &identity) != nil || len(offer.Variant) > 0 && common.Unmarshal(offer.Variant, &variant) != nil {
		return "", false
	}
	if variant.Variant == "batch" || strings.HasSuffix(identity.ModelID, ":batch") {
		if variant.Variant == "batch" && variant.ModelSlug == identity.ModelID && identity.ModelID == upstream && strings.HasSuffix(identity.ModelID, ":batch") {
			return "batch", true
		}
		return "", false
	}
	if strings.HasSuffix(upstream, ":batch") {
		return "", false
	}
	return "sync", true
}
func ConsumerPricingCatalogue(pricing []Pricing, usableGroups map[string]string) []Pricing {
	byName := make(map[string]int, len(pricing))
	for index, item := range pricing {
		byName[item.ModelName] = index
	}
	result := []Pricing{}
	positions := make(map[int]int)
	groups := slices.Sorted(maps.Keys(usableGroups))
	for sourceIndex, item := range pricing {
		for _, group := range groups {
			if !slices.Contains(item.EnableGroup, group) && !slices.Contains(item.EnableGroup, "all") {
				continue
			}
			canonical := item.ConsumerCanonicalModelsByGroup[group]
			if canonical == "" && slices.Contains(item.EnableGroup, "all") {
				canonical = item.ConsumerCanonicalModelsByGroup["all"]
			}
			if canonical == "" {
				canonical = item.ModelName
			}
			baseIndex := sourceIndex
			if canonical != item.ModelName {
				if targetIndex, exists := byName[canonical]; exists {
					baseIndex = targetIndex
				}
			}
			base := pricing[baseIndex]
			index, exists := positions[baseIndex]
			if !exists {
				index = len(result)
				positions[baseIndex] = index
				base.EnableGroup = []string{}
				base.CatalogOffersByGroup = make(map[string][]CatalogOffer)
				base.PublicCatalogOffersByGroup = make(map[string][]CatalogOffer)
				base.UpstreamCatalogRoutesByGroup = make(map[string][]UpstreamCatalogRoute)
				result = append(result, base)
			}
			if !slices.Contains(result[index].EnableGroup, group) {
				result[index].EnableGroup = append(result[index].EnableGroup, group)
			}
			result[index].UpstreamCatalogRoutesByGroup[group] = append(result[index].UpstreamCatalogRoutesByGroup[group], item.UpstreamCatalogRoutesForGroup(group)...)
			result[index].CatalogOffersByGroup[group] = append(result[index].CatalogOffersByGroup[group], item.CatalogOffersForGroup(group)...)
			result[index].PublicCatalogOffersByGroup[group] = append(result[index].PublicCatalogOffersByGroup[group], item.PublicCatalogOffersByGroup["all"]...)
			result[index].PublicCatalogOffersByGroup[group] = append(result[index].PublicCatalogOffersByGroup[group], item.PublicCatalogOffersByGroup[group]...)
		}
	}
	return result
}
