package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/QuantumNous/new-api/constant"

	"github.com/shopspring/decimal"
	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
	"iter"

	"maps"
	"math"

	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

type EffectivePricingResponse struct {
	SchemaVersion string                  `json:"schema_version"`
	UserGroup     string                  `json:"user_group"`
	Currency      string                  `json:"currency"`
	PriceKind     string                  `json:"price_kind"`
	PriceScope    string                  `json:"price_scope"`
	FX            BillingFXBasis          `json:"fx"`
	Accounting    EffectiveAccounting     `json:"accounting"`
	AutoGroups    []string                `json:"auto_groups"`
	Data          []EffectivePricingModel `json:"data"`
}
type EffectiveAccounting struct {
	QuotaPerUnit float64 `json:"quota_per_unit"`
	RubPerUnit   float64 `json:"rub_per_unit"`
	QuotaPerRub  float64 `json:"quota_per_rub"`
}
type EffectivePricingModel struct {
	ModelName              string                  `json:"model_name"`
	Description            string                  `json:"description,omitempty"`
	Icon                   string                  `json:"icon,omitempty"`
	Tags                   string                  `json:"tags,omitempty"`
	VendorID               int                     `json:"vendor_id,omitempty"`
	OwnerBy                string                  `json:"owner_by"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	GroupPrices            []EffectiveGroupPricing `json:"group_prices"`
}
type EffectiveGroupPricing struct {
	BillingSourcesByEndpoint map[constant.EndpointType][]string `json:"billing_sources_by_endpoint,omitempty"`
	UsingGroup               string                             `json:"using_group"`
	PureGroupRatio           float64                            `json:"pure_group_ratio"`
	EffectiveBillingRatio    float64                            `json:"effective_billing_ratio"`
	BillingMode              string                             `json:"billing_mode"`
	BillingSurface           string                             `json:"billing_surface"`
	Status                   string                             `json:"status"`
	UnitPrices               []EffectiveUnitPrice               `json:"unit_prices,omitempty"`
	Tiers                    []EffectivePricingTier             `json:"tiers"`
	IsFree                   bool                               `json:"is_free"`
	Formula                  *EffectivePricingFormula           `json:"formula,omitempty"`
	Limitations              []string                           `json:"limitations,omitempty"`
	UsageSchema              map[string]TariffUsageField        `json:"usage_schema,omitempty"`
}
type EffectiveUnitPrice struct {
	Component string  `json:"component"`
	Unit      string  `json:"unit"`
	AmountRub float64 `json:"amount_rub"`
}
type EffectivePricingFormula struct {
	Expression          string  `json:"expression"`
	Kind                string  `json:"kind"`
	OutputToQuotaFactor float64 `json:"output_to_quota_factor"`
	OutputToRubFactor   float64 `json:"output_to_rub_factor"`
}
type EffectivePricingCreator struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type EffectivePricingPageItem struct {
	ConsumerPricingModel
	Modality string                   `json:"modality"`
	Creator  *EffectivePricingCreator `json:"creator"`
}
type EffectivePricingPageData struct {
	ConsumerPricingResponse
	Offset        int                        `json:"offset"`
	Limit         int                        `json:"limit"`
	TotalCount    int                        `json:"total_count"`
	Creators      []EffectivePricingCreator  `json:"creators"`
	Data          []EffectivePricingPageItem `json:"data"`
	SectionCounts map[string]int             `json:"-"`
}
type EffectivePricingPageQuery struct {
	Personal           bool   `json:"-"`
	Limit              int    `json:"-"`
	Offset             int    `json:"-"`
	Search             string `json:"-"`
	Modality           string `json:"-"`
	CatalogType        string `json:"-"`
	CatalogTypeMissing bool   `json:"-"`
	CreatorID          *int   `json:"-"`
	PricingUnit        string `json:"-"`
	OrderBy            string `json:"-"`
	Direction          string `json:"-"`
	ExpectedRevision   string `json:"-"`
	Overview           bool   `json:"-"`
}
type EffectivePricingPageContext struct {
	Policy       *Projection             `json:"-"`
	UserID       int                     `json:"-"`
	Metadata     ConsumerPricingResponse `json:"-"`
	FxFactor     float64                 `json:"-"`
	Pricing      []Pricing               `json:"-"`
	Vendors      []PricingVendor         `json:"-"`
	UsableGroups map[string]string       `json:"-"`
	GroupRatios  map[string]float64      `json:"-"`
	UsingGroup   string                  `json:"-"`
}
type EffectivePricingPageCandidate struct {
	Index    int                      `json:"-"`
	Modality string                   `json:"-"`
	Creator  *EffectivePricingCreator `json:"-"`
	Price    *float64                 `json:"-"`
}
type EffectivePricingOverviewSection struct {
	Modality   string                     `json:"modality"`
	Offset     int                        `json:"offset"`
	Limit      int                        `json:"limit"`
	TotalCount int                        `json:"total_count"`
	Data       []EffectivePricingPageItem `json:"data"`
}
type EffectivePricingOverviewData struct {
	SchemaVersion   string                            `json:"schema_version"`
	CatalogRevision string                            `json:"catalog_revision"`
	UserGroup       string                            `json:"user_group"`
	Currency        string                            `json:"currency"`
	AutoGroups      []string                          `json:"auto_groups"`
	Creators        []EffectivePricingCreator         `json:"creators"`
	Sections        []EffectivePricingOverviewSection `json:"sections"`
}
type PublicPricingModelsPage struct {
	SchemaVersion   string               `json:"schema_version"`
	CatalogRevision string               `json:"catalog_revision"`
	Currency        string               `json:"currency"`
	Page            int                  `json:"p"`
	PageSize        int                  `json:"page_size"`
	TotalCount      int                  `json:"total_count"`
	Data            []PublicPricingModel `json:"data"`
}
type PublicPricingModel struct {
	CatalogType            string                   `json:"catalog_type,omitempty"`
	DisplayName            string                   `json:"display_name,omitempty"`
	ModelName              string                   `json:"model_name"`
	Description            string                   `json:"description,omitempty"`
	Icon                   string                   `json:"icon,omitempty"`
	Tags                   []string                 `json:"tags"`
	OwnerBy                string                   `json:"owner_by"`
	SupportedEndpointTypes []constant.EndpointType  `json:"supported_endpoint_types"`
	Creator                *EffectivePricingCreator `json:"creator"`
	Tariff                 PublicModelTariff        `json:"tariff"`
}
type PublicModelTariff struct {
	Operations []PublicPricingOperation `json:"operations"`
}
type PublicPricingOperation struct {
	Operation string              `json:"operation"`
	Mode      string              `json:"mode"`
	Prices    []ConsumerUnitPrice `json:"prices"`
	Tiers     []PublicPricingTier `json:"tiers"`
	IsFrom    bool                `json:"is_from"`
	IsFree    bool                `json:"is_free"`
}
type PublicPricingTier struct {
	Condition *PublicPricingCondition `json:"condition,omitempty"`
	Prices    []ConsumerUnitPrice     `json:"prices"`
}
type PublicPricingCondition struct {
	Kind        string                   `json:"kind"`
	Subject     string                   `json:"subject,omitempty"`
	Argument    string                   `json:"argument,omitempty"`
	Unit        string                   `json:"unit,omitempty"`
	Operator    string                   `json:"operator,omitempty"`
	StringValue *string                  `json:"string_value,omitempty"`
	Value       *float64                 `json:"value,omitempty"`
	Conditions  []PublicPricingCondition `json:"conditions,omitempty"`
}
type PublicPricingModelsQuery struct {
	CatalogType        string   `json:"-"`
	CatalogTypeMissing bool     `json:"-"`
	ExpectedRevision   string   `json:"-"`
	Tags               []string `json:"-"`
	Page               int      `json:"-"`
	PageSize           int      `json:"-"`
}
type ConsumerPricingModelDetail struct {
	SchemaVersion   string                   `json:"schema_version"`
	CatalogRevision string                   `json:"catalog_revision"`
	UserGroup       string                   `json:"user_group"`
	Currency        string                   `json:"currency"`
	AutoGroups      []string                 `json:"auto_groups"`
	Model           EffectivePricingPageItem `json:"model"`
}
type ConsumerPricingResponse struct {
	SchemaVersion   string                 `json:"schema_version"`
	CatalogRevision string                 `json:"catalog_revision"`
	UserGroup       string                 `json:"user_group"`
	Currency        string                 `json:"currency"`
	AutoGroups      []string               `json:"auto_groups"`
	Data            []ConsumerPricingModel `json:"data"`
}
type ConsumerPricingModel struct {
	CatalogType            string                  `json:"catalog_type,omitempty"`
	DisplayName            string                  `json:"display_name,omitempty"`
	ModelName              string                  `json:"model_name"`
	Description            string                  `json:"description,omitempty"`
	Icon                   string                  `json:"icon,omitempty"`
	Tags                   string                  `json:"tags,omitempty"`
	VendorID               int                     `json:"vendor_id,omitempty"`
	OwnerBy                string                  `json:"owner_by"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	GroupPrices            []ConsumerGroupPricing  `json:"group_prices"`
}
type ConsumerGroupPricing struct {
	ProviderOffers []ConsumerProviderOffer    `json:"provider_offers,omitempty"`
	UsingGroup     string                     `json:"using_group"`
	Operations     []ConsumerPricingOperation `json:"operations"`
}
type ConsumerPricingOperation struct {
	Tiers     []PublicPricingTier `json:"tiers,omitempty"`
	IsFrom    *bool               `json:"is_from,omitempty"`
	Operation string              `json:"operation"`
	Mode      string              `json:"mode"`
	Prices    []ConsumerUnitPrice `json:"prices"`
}
type ConsumerUnitPrice struct {
	Component string   `json:"component"`
	Unit      string   `json:"unit"`
	AmountRub *float64 `json:"amount_rub"`
}
type UpstreamRate SourceRate
type ConsumerProviderPrice struct {
	UpstreamRate
	Component string  `json:"component"`
	AmountRub *string `json:"amount_rub"`
}
type ConsumerProviderOffer struct {
	MinimumEligible bool   `json:"-"`
	DocumentHash    string `json:"-"`

	Availability  string                  `json:"catalog_availability"`
	Limitations   []string                `json:"limitations"`
	ID            string                  `json:"id"`
	UpstreamModel string                  `json:"upstream_model"`
	Operation     string                  `json:"operation"`
	Mode          string                  `json:"mode"`
	Scope         string                  `json:"scope"`
	Provider      *SourceProvider         `json:"provider"`
	Variant       json.RawMessage         `json:"variant"`
	Prices        []ConsumerProviderPrice `json:"prices"`
	Coverage      SourceCoverage          `json:"coverage"`
	Eligibility   *SourceEligibility      `json:"eligibility"`
	Capabilities  json.RawMessage         `json:"capabilities"`
	Limits        json.RawMessage         `json:"limits"`
}
type PricingUserContext struct {
	Id    int    `json:"-"`
	Group string `json:"-"`
}
type CatalogPriceWindow struct {
	ContextMin *int64 `json:"contextMin"`
	ContextMax *int64 `json:"contextMax"`
	Day        *int   `json:"utcDay"`
	Start      *int   `json:"utcStart"`
	End        *int   `json:"utcEnd"`
}
type CatalogScheduledPrice struct {
	Price           ConsumerProviderPrice    `json:"-"`
	Tier            string                   `json:"-"`
	Variant         string                   `json:"-"`
	Conditions      []PublicPricingCondition `json:"-"`
	Low, High       int64                    `json:"-"`
	Day, Start, End int                      `json:"-"`
}
type EffectivePricingTier struct {
	Label            string                         `json:"label,omitempty"`
	Condition        *EffectivePricingTierCondition `json:"condition,omitempty"`
	UnitPrices       []EffectiveUnitPrice           `json:"unit_prices"`
	PriceComposition string                         `json:"price_composition,omitempty"`
}
type EffectivePricingTierCondition struct {
	Kind        string                       `json:"kind"`
	Unit        string                       `json:"unit,omitempty"`
	MinValue    *float64                     `json:"min_value,omitempty"`
	MaxValue    *float64                     `json:"max_value,omitempty"`
	TimeZone    string                       `json:"time_zone,omitempty"`
	TimeWindows []EffectivePricingTimeWindow `json:"time_windows,omitempty"`
}
type EffectivePricingTimeWindow struct {
	StartHour int `json:"start_hour"`
	EndHour   int `json:"end_hour"`
}

// effectivePricingComponents maps expression variables onto the component
// vocabulary unit_prices already publishes, in display order. Variables outside
// the table are published under their own name.
var EffectivePricingComponents = []struct{ variable, component string }{
	{"p", "input"},
	{"c", "output"},
	{"cr", "cache_read"},
	{"cc", "cache_write"},
	{"cc1h", "cache_write_1h"},
	{"img", "image_input"},
	{"img_o", "image_output"},
	{"ai", "audio_input"},
	{"ao", "audio_output"},
}

const CatalogMaxScheduleCells = 4096
const PricingCatalogMaxContexts = 4096
const HoursPerDay = 24
const CatalogUnboundedContext int64 = 9007199254740991

var PricingSpeechFamily = regexp.MustCompile(`(^tts-)|(-tts(-|$))`)
var TierConditionKinds = map[string]string{
	"len": "context_length",
	"p":   "input_tokens",
	"c":   "output_tokens",
}
var PricingTranscriptionFamily = regexp.MustCompile(`(^whisper-)|(-transcribe(-|$))`)
var PricingPageInteger = regexp.MustCompile(`^-?[0-9]+$`)

func FilterPricingByUsableGroups(pricing []Pricing, usableGroup map[string]string) []Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []Pricing{}
	}

	filtered := make([]Pricing, 0, len(pricing))
	for _, item := range pricing {
		if slices.Contains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}
func (pricingPolicy *Projection) BuildEffectivePricingResponse(userGroup string, basis BillingFXBasis, pricing []Pricing) EffectivePricingResponse {
	usableGroups := pricingPolicy.runtime.UsableGroups(userGroup)
	pricing = FilterPricingByUsableGroups(pricing, usableGroups)
	response := EffectivePricingResponse{
		SchemaVersion: "v1",
		UserGroup:     userGroup,
		Currency:      "RUB",
		PriceKind:     "current_tariff",
		PriceScope:    "user_group",
		FX:            basis,
		Accounting: EffectiveAccounting{
			QuotaPerUnit: pricingPolicy.runtime.QuotaUnit(),
			RubPerUnit:   100,
			QuotaPerRub:  float64(pricingPolicy.runtime.QuotaUnit()) / 100,
		},
		AutoGroups: pricingPolicy.runtime.AutoGroups(userGroup),
		Data:       make([]EffectivePricingModel, 0, len(pricing)),
	}
	for _, item := range pricing {
		response.Data = append(response.Data, pricingPolicy.BuildEffectivePricingModel(userGroup, usableGroups, basis.Factor, item))
	}
	return response
}
func (pricingPolicy *Projection) BuildEffectivePricingModel(userGroup string, usableGroups map[string]string, fxFactor float64, item Pricing) EffectivePricingModel {
	groupRatios := make(map[string]float64, len(usableGroups))
	for group := range usableGroups {
		groupRatios[group] = pricingPolicy.runtime.GroupRatio(userGroup, group)
	}
	return pricingPolicy.BuildEffectivePricingModelWithRatios(usableGroups, groupRatios, fxFactor, item)
}
func (pricingPolicy *Projection) BuildEffectivePricingModelWithRatios(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item Pricing) EffectivePricingModel {
	result := EffectivePricingModel{
		ModelName:              item.ModelName,
		Description:            item.Description,
		Icon:                   item.Icon,
		Tags:                   item.Tags,
		VendorID:               item.VendorID,
		OwnerBy:                item.OwnerBy,
		SupportedEndpointTypes: item.SupportedEndpointTypes,
	}
	groups := EffectivePricingGroups(item.EnableGroup, usableGroups)
	result.GroupPrices = make([]EffectiveGroupPricing, 0, len(groups))
	for _, group := range groups {
		pure := groupRatios[group]
		effective, err := pricingPolicy.runtime.ApplyFX(pure, fxFactor)
		groupPrice := pricingPolicy.BuildEffectiveGroupPricing(item, group, pure, effective)
		sourcesByEndpoint := item.BillingSourcesForGroup(group)
		groupPrice.BillingSourcesByEndpoint = sourcesByEndpoint

		if err != nil {
			groupPrice.Status = "unavailable"
			groupPrice.Limitations = append(groupPrice.Limitations, "invalid_group_ratio")
		}
		result.GroupPrices = append(result.GroupPrices, groupPrice)
	}
	return result
}
func EffectivePricingGroups(enableGroups []string, usableGroups map[string]string) []string {
	if slices.Contains(enableGroups, "all") {
		groups := make([]string, 0, len(usableGroups))
		for group := range usableGroups {
			groups = append(groups, group)
		}
		return groups
	}
	groups := make([]string, 0, len(enableGroups))
	for _, group := range enableGroups {
		if _, ok := usableGroups[group]; ok {
			groups = append(groups, group)
		}
	}
	return groups
}
func (pricingPolicy *Projection) BuildEffectiveGroupPricing(item Pricing, group string, pureRatio, effectiveRatio float64) EffectiveGroupPricing {
	groupPrice := EffectiveGroupPricing{
		UsingGroup:            group,
		PureGroupRatio:        pureRatio,
		EffectiveBillingRatio: effectiveRatio,
		BillingMode:           BillingModeRatio,
		BillingSurface:        "token",
	}
	if item.BillingMode == BillingModeTieredExpr && strings.TrimSpace(item.BillingExpr) != "" {
		groupPrice.BillingMode = item.BillingMode
		kind := "token_expression"
		outputToQuota := float64(pricingPolicy.runtime.QuotaUnit()) * effectiveRatio / 1_000_000
		outputToRub := outputToQuota / (float64(pricingPolicy.runtime.QuotaUnit()) / 100)
		if len(item.BillingUsageSchema) > 0 {
			kind = "task_usage_expression"
			groupPrice.BillingSurface = "task"
			outputToQuota = float64(pricingPolicy.runtime.QuotaUnit()) * effectiveRatio
			outputToRub = outputToQuota / (float64(pricingPolicy.runtime.QuotaUnit()) / 100)
			groupPrice.UsageSchema = make(map[string]TariffUsageField, len(item.BillingUsageSchema))
			for key, schema := range item.BillingUsageSchema {
				groupPrice.UsageSchema[key] = schema
			}
		}
		groupPrice.Status = "formula"
		groupPrice.Formula = &EffectivePricingFormula{Expression: item.BillingExpr, Kind: kind, OutputToQuotaFactor: outputToQuota, OutputToRubFactor: outputToRub}
		groupPrice.SetTiers([]EffectivePricingTier{})
		if kind == "token_expression" {
			groupPrice.SetTiers(TokenExpressionTiers(pricingPolicy.runtime.Capabilities, item.BillingExpr, outputToRub))
		}
		return groupPrice
	}
	if item.QuotaType == 1 {
		groupPrice.BillingSurface = "per_call"
		groupPrice.Status = "unit_prices"
		groupPrice.UnitPrices = []EffectiveUnitPrice{{Component: "base", Unit: "call", AmountRub: item.ModelPrice * 100 * effectiveRatio}}
		groupPrice.Limitations = []string{"provider-specific multipliers may apply for some request parameters"}
		groupPrice.SetTiers([]EffectivePricingTier{{UnitPrices: groupPrice.UnitPrices}})
		return groupPrice
	}
	groupPrice.Status = "unit_prices"
	baseInput := item.ModelRatio * effectiveRatio * 200
	groupPrice.UnitPrices = []EffectiveUnitPrice{
		{Component: "input", Unit: "million_tokens", AmountRub: baseInput},
		{Component: "output", Unit: "million_tokens", AmountRub: baseInput * item.CompletionRatio},
	}
	if item.CacheRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, EffectiveUnitPrice{Component: "cache_read", Unit: "million_tokens", AmountRub: baseInput * *item.CacheRatio})
	}
	if item.CreateCacheRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, EffectiveUnitPrice{Component: "cache_write", Unit: "million_tokens", AmountRub: baseInput * *item.CreateCacheRatio})
	}
	if item.ImageRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, EffectiveUnitPrice{Component: "image_input", Unit: "million_tokens", AmountRub: baseInput * *item.ImageRatio})
	}
	if item.AudioRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, EffectiveUnitPrice{Component: "audio_input", Unit: "million_tokens", AmountRub: baseInput * *item.AudioRatio})
	}
	if item.AudioCompletionRatio != nil {
		audioInput := baseInput
		if item.AudioRatio != nil {
			audioInput *= *item.AudioRatio
		}
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, EffectiveUnitPrice{Component: "audio_output", Unit: "million_tokens", AmountRub: audioInput * *item.AudioCompletionRatio})
	}
	groupPrice.SetTiers([]EffectivePricingTier{{UnitPrices: groupPrice.UnitPrices}})
	return groupPrice
}
func (context EffectivePricingPageContext) Project(item Pricing) ConsumerPricingModel {
	pricingPolicy := context.Policy
	return pricingPolicy.BuildConsumerPricingModel(context.UsableGroups, context.GroupRatios, context.FxFactor, item, false, PricingUserContext{Id: context.UserID, Group: context.Metadata.UserGroup})
}
func (pricingPolicy *Projection) BuildEffectivePricingPage(context EffectivePricingPageContext, query EffectivePricingPageQuery, Project func(Pricing) ConsumerPricingModel) EffectivePricingPageData {
	context.Pricing = ConsumerPricingCatalogue(context.Pricing, context.UsableGroups)
	response := EffectivePricingPageData{ConsumerPricingResponse: context.Metadata, Offset: query.Offset, Limit: query.Limit, Creators: []EffectivePricingCreator{}, Data: []EffectivePricingPageItem{}}
	creators := make(map[int]*EffectivePricingCreator, len(context.Vendors))
	for _, vendor := range context.Vendors {
		if name := TrimPricingText(vendor.Name); name != "" {
			creators[vendor.ID] = &EffectivePricingCreator{ID: vendor.ID, Name: name}
		}
	}
	usedCreators := make(map[int]bool)
	candidates := make([]EffectivePricingPageCandidate, 0, len(context.Pricing))
	search := NormalizeModelSearch(query.Search)
	for index, item := range context.Pricing {
		if len(EffectivePricingGroups(item.EnableGroup, context.UsableGroups)) == 0 {
			continue
		}
		if context.UsingGroup != "" && len(item.BillingSourcesForGroup(context.UsingGroup)) == 0 {
			if _, err := pricingPolicy.runtime.ApplyFX(context.GroupRatios[context.UsingGroup], context.FxFactor); err != nil {
				continue
			}
		}
		if query.CatalogTypeMissing && item.CatalogType != "" || query.CatalogType != "" && item.CatalogType != query.CatalogType {
			continue
		}
		creator := creators[item.VendorID]
		if item.VendorID == 0 {
			creator = nil
		}
		if creator != nil && !usedCreators[creator.ID] {
			response.Creators = append(response.Creators, *creator)
			usedCreators[creator.ID] = true
		}
		modality := EffectivePricingModality(item)
		if search != "" && !strings.Contains(NormalizeModelSearch(item.ModelName), search) && !strings.Contains(NormalizeModelSearch(item.DisplayName), search) {
			continue
		}
		if query.Modality != "" && modality != query.Modality {
			continue
		}
		if query.CreatorID != nil && (creator == nil || creator.ID != *query.CreatorID) {
			continue
		}
		candidate := EffectivePricingPageCandidate{Index: index, Creator: creator, Modality: modality}
		if query.PricingUnit != "" || query.OrderBy != "NAME" {
			projected := Project(item)
			unit, price := EffectivePricingPageKeys(projected, query.OrderBy)
			if query.PricingUnit != "" && unit != query.PricingUnit {
				continue
			}
			candidate.Price = price
		}
		candidates = append(candidates, candidate)
	}
	collator := collate.New(language.English)
	slices.SortFunc(response.Creators, func(left, right EffectivePricingCreator) int {
		if order := collator.CompareString(left.Name, right.Name); order != 0 {
			return order
		}
		return left.ID - right.ID
	})
	slices.SortStableFunc(candidates, func(left, right EffectivePricingPageCandidate) int {
		if query.OrderBy != "NAME" {
			if left.Price == nil && right.Price != nil {
				return 1
			}
			if left.Price != nil && right.Price == nil {
				return -1
			}
			if left.Price != nil && right.Price != nil && *left.Price != *right.Price {
				order := -1
				if *left.Price > *right.Price {
					order = 1
				}
				if query.Direction == "desc" {
					order = -order
				}
				return order
			}
		}
		order := CompareEffectivePricingNames(context.Pricing[left.Index].ModelName, context.Pricing[right.Index].ModelName)
		if query.OrderBy == "NAME" && query.Direction == "desc" {
			return -order
		}
		return order
	})
	response.TotalCount = len(candidates)
	if query.Overview {
		response.SectionCounts = make(map[string]int)
		for _, candidate := range candidates {
			response.SectionCounts[candidate.Modality]++
			if response.SectionCounts[candidate.Modality] <= query.Limit {
				response.Data = append(response.Data, EffectivePricingPageItem{ConsumerPricingModel: Project(context.Pricing[candidate.Index]), Modality: candidate.Modality, Creator: candidate.Creator})
			}
		}
		return response
	}
	start := min(query.Offset, len(candidates))
	end := start + min(query.Limit, len(candidates)-start)
	for _, candidate := range candidates[start:end] {
		response.Data = append(response.Data, EffectivePricingPageItem{ConsumerPricingModel: Project(context.Pricing[candidate.Index]), Modality: candidate.Modality, Creator: candidate.Creator})
	}
	return response
}
func CompareEffectivePricingNames(left, right string) int {
	lower := cases.Lower(language.Und)
	if order := slices.Compare(utf16.Encode([]rune(lower.String(left))), utf16.Encode([]rune(lower.String(right)))); order != 0 {
		return order
	}
	return slices.Compare(utf16.Encode([]rune(left)), utf16.Encode([]rune(right)))
}
func EffectivePricingModality(item Pricing) string {
	if item.CatalogModality != "" {
		return item.CatalogModality
	}
	proved := make(map[string]bool)
	for _, offers := range item.CatalogOffersByGroup {
		for _, offer := range offers {
			switch offer.Operation {
			case "image_generation", "image_editing":
				proved["IMAGE"] = true
			case "text_to_video", "image_to_video", "video_to_video", "video_editing", "video_upscaling", "remix":
				proved["VIDEO"] = true
			case "embeddings":
				proved["EMBEDDINGS"] = true
			case "rerank":
				proved["RERANK"] = true
			case "speech_generation", "music_generation", "transcription", "realtime":
				proved["OTHER"] = true
			case "text":
				proved["TEXT"] = true
			}
		}
	}
	for _, modality := range []string{"IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER", "TEXT"} {
		if proved[modality] {
			return modality
		}
	}
	endpoints := make([]string, 0, len(item.SupportedEndpointTypes))
	name := strings.ToLower(item.ModelName[strings.LastIndex(item.ModelName, "/")+1:])
	for _, endpoint := range item.SupportedEndpointTypes {
		normalized := strings.ToLower(string(endpoint))
		if normalized == "openai" {
			switch {
			case strings.HasPrefix(name, "sora-"):
				normalized = "openai-video"
			case strings.HasPrefix(name, "dall-e-"), strings.HasPrefix(name, "gpt-image-"), strings.HasPrefix(name, "chatgpt-image-"):
				normalized = "image-generation"
			case strings.HasPrefix(name, "text-embedding-"):
				normalized = "embeddings"
			case PricingSpeechFamily.MatchString(name):
				normalized = "audio-speech"
			case PricingTranscriptionFamily.MatchString(name):
				normalized = "audio-transcription"
			}
		}
		endpoints = append(endpoints, normalized)
	}
	for _, category := range []struct {
		fragment, Modality string `json:"-"`
	}{{"image", "IMAGE"}, {"video", "VIDEO"}, {"embedding", "EMBEDDINGS"}, {"rerank", "RERANK"}, {"audio", "OTHER"}} {
		for _, endpoint := range endpoints {
			if strings.Contains(endpoint, category.fragment) {
				return category.Modality
			}
		}
	}
	return "TEXT"
}
func EffectivePricingPageKeys(item ConsumerPricingModel, orderBy string) (string, *float64) {
	unit := "UNCONFIGURED"
	var minimum *float64
	component := "input"
	if orderBy == "OUTPUT_PRICE" {
		component = "output"
	}
	for _, group := range item.GroupPrices {
		for _, operation := range group.Operations {
			if operation.Mode == "batch" {
				continue
			}
			for _, price := range operation.Prices {
				if price.AmountRub == nil {
					continue
				}
				if unit == "UNCONFIGURED" {
					unit = "NATIVE"
				}
				if price.Unit == "call" {
					unit = "CALLS"
				} else if price.Unit == "million_tokens" && unit != "CALLS" {
					unit = "TOKENS"
				}
				if price.Component == component && price.Unit == "million_tokens" && (minimum == nil || *price.AmountRub < *minimum) {
					amount := *price.AmountRub
					minimum = &amount
				}
			}
		}
	}
	return unit, minimum
}
func NormalizeModelSearch(value string) string {
	value = cases.Lower(language.Und).String(norm.NFKC.String(TrimPricingText(value)))
	normalized := strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character) {
			return character
		}
		return -1
	}, value)
	if normalized == "" {
		return value
	}
	return normalized
}
func TrimPricingText(value string) string {
	return strings.TrimFunc(value, func(character rune) bool {
		return character == '\ufeff' || (character != '\u0085' && unicode.IsSpace(character))
	})
}

// NormalizePricingSearch folds the search text of an effective pricing page.
func NormalizePricingSearch(value string) string {
	return cases.Lower(language.Und).String(TrimPricingText(value))
}

func (pricingPolicy *Projection) BuildEffectivePricingOverview(context EffectivePricingPageContext, query EffectivePricingPageQuery) EffectivePricingOverviewData {
	query.Overview = true
	page := pricingPolicy.BuildEffectivePricingPage(context, query, context.Project)
	result := EffectivePricingOverviewData{SchemaVersion: context.Metadata.SchemaVersion, CatalogRevision: context.Metadata.CatalogRevision, UserGroup: context.Metadata.UserGroup, Currency: context.Metadata.Currency, AutoGroups: context.Metadata.AutoGroups, Creators: page.Creators, Sections: []EffectivePricingOverviewSection{}}
	for _, modality := range []string{"TEXT", "IMAGE", "VIDEO", "EMBEDDINGS", "RERANK", "OTHER"} {
		section := EffectivePricingOverviewSection{Modality: modality, Limit: query.Limit, TotalCount: page.SectionCounts[modality], Data: []EffectivePricingPageItem{}}
		for _, item := range page.Data {
			if item.Modality == modality {
				section.Data = append(section.Data, item)
			}
		}
		result.Sections = append(result.Sections, section)
	}
	return result
}
func PricingTags(value string) []string {
	tags := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || unicode.IsSpace(r)
	})
	slices.Sort(tags)
	return slices.Compact(tags)
}
func (pricingPolicy *Projection) BuildPublicPricingModelsPage(context EffectivePricingPageContext, query PublicPricingModelsQuery) PublicPricingModelsPage {
	response := PublicPricingModelsPage{SchemaVersion: "v1", CatalogRevision: context.Metadata.CatalogRevision, Currency: "RUB", Page: query.Page, PageSize: query.PageSize, Data: []PublicPricingModel{}}

	context.UsableGroups = map[string]string{"default": "default"}
	pricing := ConsumerPricingCatalogue(context.Pricing, context.UsableGroups)
	candidates := make([]Pricing, 0, len(pricing))
	for _, item := range pricing {
		if query.CatalogTypeMissing && item.CatalogType != "" || query.CatalogType != "" && item.CatalogType != query.CatalogType {
			continue
		}
		tags := PricingTags(item.Tags)
		if !slices.ContainsFunc(query.Tags, func(tag string) bool { return !slices.Contains(tags, tag) }) {
			candidates = append(candidates, item)
		}
	}
	slices.SortFunc(candidates, func(a, b Pricing) int { return CompareEffectivePricingNames(a.ModelName, b.ModelName) })
	response.TotalCount = len(candidates)
	start := len(candidates)
	if query.Page-1 <= len(candidates)/query.PageSize {
		start = min((query.Page-1)*query.PageSize, len(candidates))
	}
	end := start + min(query.PageSize, len(candidates)-start)
	creators := make(map[int]*EffectivePricingCreator, len(context.Vendors))
	for _, vendor := range context.Vendors {
		if name := TrimPricingText(vendor.Name); vendor.ID != 0 && name != "" {
			creators[vendor.ID] = &EffectivePricingCreator{ID: vendor.ID, Name: name}
		}
	}
	for _, item := range candidates[start:end] {
		response.Data = append(response.Data, PublicPricingModel{CatalogType: item.CatalogType, DisplayName: item.DisplayName, ModelName: item.ModelName, Description: item.Description, Icon: item.Icon, Tags: PricingTags(item.Tags), OwnerBy: item.OwnerBy, SupportedEndpointTypes: append([]constant.EndpointType{}, item.SupportedEndpointTypes...), Creator: creators[item.VendorID], Tariff: pricingPolicy.BuildPublicModelTariff(context, item)})
	}
	return response
}
func PublicPricingGuard(guard ExpressionCondition, units ...map[string]string) PublicPricingCondition {
	condition := PublicPricingCondition{Kind: "comparison", Subject: guard.Var, Argument: guard.Arg, Operator: guard.Op, Value: &guard.Value}
	if guard.Var == "len" {
		condition.Subject = "context_length"
		condition.Unit = "token"
	} else if ComponentName(guard.Var) != guard.Var || guard.Var == "p" || guard.Var == "c" {
		condition.Subject = ComponentName(guard.Var)
		if len(units) > 0 {
			condition.Unit = units[0][condition.Subject]
		}
	}
	if guard.Op == "&&" || guard.Op == "||" {
		condition = PublicPricingCondition{Kind: "all_of"}
		if guard.Op == "||" {
			condition.Kind = "any_of"
		}
		for _, operand := range guard.Operands {
			condition.Conditions = append(condition.Conditions, PublicPricingGuard(operand, units...))
		}
	}
	if guard.Negated {
		condition = PublicPricingCondition{Kind: "not", Conditions: []PublicPricingCondition{condition}}
	}
	return condition
}
func (pricingPolicy *Projection) BuildPublicModelTariff(context EffectivePricingPageContext, item Pricing) PublicModelTariff {
	return pricingPolicy.BuildCatalogModelTariff(context, item, "default")
}
func (pricingPolicy *Projection) BuildCatalogModelTariff(context EffectivePricingPageContext, item Pricing, group string) PublicModelTariff {
	result := PublicModelTariff{Operations: []PublicPricingOperation{}}
	offers := item.PublicCatalogOffersByGroup[group]
	if len(offers) == 0 {
		offers = item.CatalogOffersForGroup(group)
	}
	byOperation := make(map[string][]CatalogOffer)
	for _, offer := range offers {
		key := offer.Operation + ":" + offer.Mode
		byOperation[key] = append(byOperation[key], offer)
	}
	keys := make([]string, 0, len(byOperation))
	for key := range byOperation {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ratio, ratioErr := pricingPolicy.runtime.ApplyFX(context.GroupRatios[group], context.FxFactor)
	for _, key := range keys {
		operation, mode, _ := strings.Cut(key, ":")
		row := PublicPricingOperation{Operation: operation, Mode: mode, Prices: []ConsumerUnitPrice{}, Tiers: []PublicPricingTier{}, IsFree: true}
		candidates := byOperation[key]
		selected := SelectCatalogOffer(candidates)
		sources := item.BillingSourcesForGroup(group)
		actualCost := false
		for _, values := range sources {
			actualCost = actualCost || slices.Contains(values, BillingSourceOpenRouterActualCost)
		}
		row.IsFrom = actualCost || len(candidates) > 1
		unknown := ratioErr != nil
		seen := make(map[string]bool)
		for _, offer := range candidates {
			tier := PublicPricingTier{Prices: []ConsumerUnitPrice{}}
			if len(offer.PublicConditions) > 0 || len(offer.PublicUsageConditions) > 0 {
				condition := PublicPricingCondition{Kind: "all_of"}
				units := make(map[string]string)
				for _, price := range offer.Prices {
					if price.Unit == "million_tokens" {
						units[price.Component] = "token"
					} else if price.Unit == "million_characters" {
						units[price.Component] = "character"
					}
				}
				for _, guard := range offer.PublicConditions {
					condition.Conditions = append(condition.Conditions, PublicPricingGuard(guard, units))
				}
				for _, guard := range offer.PublicUsageConditions {
					comparison := PublicPricingCondition{Kind: "comparison", Subject: "usage", Argument: guard.Field, Operator: "==", StringValue: &guard.Value}
					if guard.Negated {
						comparison = PublicPricingCondition{Kind: "not", Conditions: []PublicPricingCondition{comparison}}
					}
					condition.Conditions = append(condition.Conditions, comparison)
				}
				if len(condition.Conditions) == 1 {
					condition = condition.Conditions[0]
				}
				tier.Condition = &condition
			}
			row.IsFrom = row.IsFrom || offer.PublicConditional || tier.Condition != nil
			if len(offer.Prices) == 0 {
				unknown = true
			}
			for _, price := range offer.Prices {
				public := ConsumerUnitPrice{Component: price.Component, Unit: price.Unit}
				if ratioErr == nil && price.AmountUSD != nil && price.Unit != "" && price.Unit != "unknown" {
					amount := *price.AmountUSD * 100 * ratio
					if amount >= 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0) {
						public.AmountRub = &amount
					}
				}
				if public.AmountRub == nil {
					unknown = true
				} else if *public.AmountRub != 0 {
					row.IsFree = false
				}
				tier.Prices = append(tier.Prices, public)
			}
			encoded, err := pricingPolicy.runtime.Capabilities.Codec.Marshal(tier)
			if err == nil && !seen[string(encoded)] {
				row.Tiers = append(row.Tiers, tier)
				seen[string(encoded)] = true
			}
			if selected != nil && selected.ID == offer.ID {
				row.Prices = tier.Prices
			}
		}
		row.IsFree = row.IsFree && !unknown
		result.Operations = append(result.Operations, row)
	}
	minima := CatalogMinimumOperations(pricingPolicy.runtime.Capabilities, pricingPolicy.IterateUpstreamProviderOffers(item, group, ratio, ratioErr == nil, PricingUserContext{}))
	for _, operation := range minima {
		index := slices.IndexFunc(result.Operations, func(row PublicPricingOperation) bool {
			return row.Operation == operation.Operation && row.Mode == operation.Mode
		})
		if index < 0 {
			result.Operations = append(result.Operations, PublicPricingOperation{Operation: operation.Operation, Mode: operation.Mode, Prices: []ConsumerUnitPrice{}, Tiers: []PublicPricingTier{}})
			index = len(result.Operations) - 1
		}
		row := &result.Operations[index]
		row.Tiers = append(row.Tiers, operation.Tiers...)
		merged := MergeCatalogMinima([]ConsumerPricingOperation{{Operation: row.Operation, Mode: row.Mode, Prices: row.Prices}}, []ConsumerPricingOperation{operation})
		row.Prices = merged[0].Prices
		row.IsFrom = row.IsFrom || *merged[0].IsFrom

		row.IsFree = false
	}
	result.Operations = slices.DeleteFunc(result.Operations, func(operation PublicPricingOperation) bool {
		return item.CatalogModality != "" && operation.Mode == "batch" && !ConfirmedConsumerPrices(operation.Prices, operation.Tiers)
	})
	return result
}
func (pricingPolicy *Projection) BuildEffectivePricingModelDetail(context EffectivePricingPageContext, name string) (ConsumerPricingModelDetail, bool) {
	for _, item := range ConsumerPricingCatalogue(context.Pricing, context.UsableGroups) {
		if item.ModelName != name || len(EffectivePricingGroups(item.EnableGroup, context.UsableGroups)) == 0 {
			continue
		}
		projected := pricingPolicy.BuildConsumerPricingModel(context.UsableGroups, context.GroupRatios, context.FxFactor, item, true, PricingUserContext{Id: context.UserID, Group: context.Metadata.UserGroup})
		var creator *EffectivePricingCreator
		for _, vendor := range context.Vendors {
			if item.VendorID != 0 && vendor.ID == item.VendorID {
				if name := TrimPricingText(vendor.Name); name != "" {
					creator = &EffectivePricingCreator{ID: vendor.ID, Name: name}
				}
				break
			}
		}
		return ConsumerPricingModelDetail{
			SchemaVersion: context.Metadata.SchemaVersion, CatalogRevision: context.Metadata.CatalogRevision,
			UserGroup: context.Metadata.UserGroup, Currency: context.Metadata.Currency, AutoGroups: context.Metadata.AutoGroups,
			Model: EffectivePricingPageItem{ConsumerPricingModel: projected, Modality: EffectivePricingModality(item), Creator: creator},
		}, true
	}
	return ConsumerPricingModelDetail{}, false
}
func (pricingPolicy *Projection) BuildConsumerPricingResponse(userGroup string, basis BillingFXBasis, pricing []Pricing) ConsumerPricingResponse {
	usableGroups := pricingPolicy.runtime.UsableGroups(userGroup)
	groupRatios := make(map[string]float64, len(usableGroups))
	for group := range usableGroups {
		groupRatios[group] = pricingPolicy.runtime.GroupRatio(userGroup, group)
	}
	response := ConsumerPricingResponse{SchemaVersion: "v2", UserGroup: userGroup, Currency: "RUB", AutoGroups: pricingPolicy.runtime.AutoGroups(userGroup), Data: make([]ConsumerPricingModel, 0, len(pricing))}
	for _, item := range FilterPricingByUsableGroups(ConsumerPricingCatalogue(pricing, usableGroups), usableGroups) {
		response.Data = append(response.Data, pricingPolicy.BuildConsumerPricingModel(usableGroups, groupRatios, basis.Factor, item, false, PricingUserContext{Group: userGroup}))
	}
	return response
}
func (pricingPolicy *Projection) BuildConsumerPricingModel(usableGroups map[string]string, groupRatios map[string]float64, fxFactor float64, item Pricing, details bool, users ...PricingUserContext) ConsumerPricingModel {
	result := ConsumerPricingModel{CatalogType: item.CatalogType, DisplayName: item.DisplayName, ModelName: item.ModelName, Description: item.Description, Icon: item.Icon, Tags: ConsumerPricingTags(item.Tags), VendorID: item.VendorID, OwnerBy: item.OwnerBy, SupportedEndpointTypes: item.SupportedEndpointTypes, GroupPrices: []ConsumerGroupPricing{}}
	groups := EffectivePricingGroups(item.EnableGroup, usableGroups)
	slices.Sort(groups)
	for _, group := range groups {
		ratio, err := pricingPolicy.runtime.ApplyFX(groupRatios[group], fxFactor)
		user := PricingUserContext{}
		if len(users) > 0 {
			user = users[0]
		}
		var providers []ConsumerProviderOffer
		var minima []ConsumerPricingOperation
		if details {
			offers := pricingPolicy.UpstreamProviderOffers(item, group, ratio, err == nil, user)
			minima = CatalogMinima(pricingPolicy.runtime.Capabilities, offers)
			providers = CatalogProviderDisplayOffers(pricingPolicy.runtime.Capabilities, offers)
		} else {
			minima = CatalogMinimumOperations(pricingPolicy.runtime.Capabilities, pricingPolicy.IterateUpstreamProviderOffers(item, group, ratio, err == nil, user))
		}
		operations := MergeCatalogMinima(ConsumerOperations(item, group, ratio, err == nil), minima)
		if slices.ContainsFunc(item.PublicCatalogOffersByGroup[group], func(offer CatalogOffer) bool { return len(offer.PublicUsageConditions) > 0 }) {
			tariff := pricingPolicy.BuildCatalogModelTariff(EffectivePricingPageContext{GroupRatios: groupRatios, FxFactor: fxFactor}, item, group)
			for index := range operations {
				for _, operation := range tariff.Operations {
					if operations[index].Operation == operation.Operation && operations[index].Mode == operation.Mode {
						operations[index].Tiers = operation.Tiers
						operations[index].IsFrom = &operation.IsFrom
					}
				}
			}
		}
		operations = slices.DeleteFunc(operations, func(operation ConsumerPricingOperation) bool {
			return item.CatalogModality != "" && operation.Mode == "batch" && !ConfirmedConsumerPrices(operation.Prices, operation.Tiers)
		})
		result.GroupPrices = append(result.GroupPrices, ConsumerGroupPricing{UsingGroup: group, Operations: operations, ProviderOffers: providers})
	}
	return result
}

// ConsumerPricingTags keeps stored comma-separated model tags intact: each tag is
// trimmed and case-insensitive duplicates keep the first original spelling.
func ConsumerPricingTags(value string) string {
	tags := make([]string, 0)
	for tag := range strings.SplitSeq(value, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" || slices.ContainsFunc(tags, func(seen string) bool { return strings.EqualFold(seen, tag) }) {
			continue
		}
		tags = append(tags, tag)
	}
	return strings.Join(tags, ",")
}

func ConfirmedConsumerPrices(prices []ConsumerUnitPrice, tiers []PublicPricingTier) bool {
	for _, price := range prices {
		if price.AmountRub != nil && *price.AmountRub >= 0 && !math.IsNaN(*price.AmountRub) && !math.IsInf(*price.AmountRub, 0) && price.Unit != "" && price.Unit != "unknown" {
			return true
		}
	}
	for _, tier := range tiers {
		if ConfirmedConsumerPrices(tier.Prices, nil) {
			return true
		}
	}
	return false
}
func ConsumerOperations(item Pricing, group string, ratio float64, validRatio bool) []ConsumerPricingOperation {
	candidates := make(map[string][]CatalogOffer)
	catalogOffers := item.CatalogOffersForGroup(group)
	for _, offer := range catalogOffers {
		key := offer.Operation + ":" + offer.Mode
		candidates[key] = append(candidates[key], offer)
	}
	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]ConsumerPricingOperation, 0, len(keys))
	for _, key := range keys {
		operation, mode, _ := strings.Cut(key, ":")
		selected := SelectCatalogOffer(candidates[key])
		row := ConsumerPricingOperation{Operation: operation, Mode: mode, Prices: []ConsumerUnitPrice{}}
		if selected != nil && validRatio {
			for _, price := range selected.Prices {
				if price.AmountUSD == nil || price.Unit == "" || price.Unit == "unknown" || *price.AmountUSD < 0 {
					continue
				}
				amount := *price.AmountUSD * 100 * ratio
				if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
					continue
				}
				row.Prices = append(row.Prices, ConsumerUnitPrice{Component: price.Component, Unit: price.Unit, AmountRub: &amount})
			}
		}
		components := []string{"base", "input", "output", "cache_read", "cache_write", "cache_write_1h", "image_input", "image_output", "audio_input", "audio_cache_read", "audio_output", "video_output", "reference_input", "additional_reference_input", "font_input"}
		slices.SortStableFunc(row.Prices, func(left, right ConsumerUnitPrice) int {
			leftIndex, rightIndex := slices.Index(components, left.Component), slices.Index(components, right.Component)
			if leftIndex < 0 {
				leftIndex = len(components)
			}
			if rightIndex < 0 {
				rightIndex = len(components)
			}
			if leftIndex != rightIndex {
				return leftIndex - rightIndex
			}
			return strings.Compare(left.Component, right.Component)
		})
		result = append(result, row)
	}
	return result
}
func ConsumerOperation(endpoint constant.EndpointType) (string, string) {
	switch endpoint {
	case PricingEndpointBatch, constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact:
		return "text", "output"
	case constant.EndpointTypeJinaRerank:
		return "rerank", "input"
	case constant.EndpointTypeEmbeddings:
		return "embeddings", "input"
	case constant.EndpointTypeImageGeneration:
		return "image_generation", "image_output"
	case constant.EndpointTypeOpenAIVideo:
		return "text_to_video", "video_output"
	}
	return "", ""
}
func CatalogComponentName(rate SourceRate) string {
	billable := ""
	if rate.Billable != nil {
		billable = *rate.Billable
	}
	component, direction, modality := "", "", ""
	switch billable {
	case "input_text", "input_tokens":
		component, direction, modality = "input", "input", "text"
	case "output_text", "output_tokens":
		component, direction, modality = "output", "output", "text"
	case "input_cache_read", "cached_input":
		component, direction, modality = "cache_read", "input", "text"
	case "input_cache_write":
		component, direction, modality = "cache_write", "input", "text"
	case "input_cache_write_1h":
		component, direction, modality = "cache_write_1h", "input", "text"
	case "web_search":
		component = "web_search"
	case "minimum_generation_charge":
		component = "minimum_charge"
	case "input_image", "image", "image_token":
		component, direction, modality = "input_image", "input", "image"
	case "image_output", "output_image":
		component, direction, modality = "output_image", "output", "image"
	case "audio", "input_audio":
		component, direction, modality = "input_audio", "input", "audio"
	case "input_audio_cache":
		component, direction, modality = "input_audio_cache", "input", "audio"
	case "audio_output", "output_audio":
		component, direction, modality = "output_audio", "output", "audio"
	case "output_video":
		component, direction, modality = "output_video", "output", "video"
	case "output_speech":
		component, direction, modality = "output_speech", "output", "speech"
	case "output_song":
		component, direction = "output_song", "output"
	case "internal_reasoning":
		component, direction, modality = "reasoning", "output", "text"
	case "search":
		component, modality = "search", "rerank"
	}
	if component == "" {
		if billable != "" {
			return "unknown"
		}

		switch rate.Parameter {
		case "prompt", "input":
			component, direction, modality = "input", "input", "text"
		case "completion", "output":
			component, direction, modality = "output", "output", "text"
		case "input_cache_read", "cached_input":
			component, direction, modality = "cache_read", "input", "text"
		case "input_cache_write":
			component, direction, modality = "cache_write", "input", "text"
		case "input_cache_write_1h":
			component, direction, modality = "cache_write_1h", "input", "text"
		case "request":
			component = "base"
		case "web_search":
			component = "web_search"
		default:
			return "unknown"
		}
	}
	if direction != "" && rate.Direction != nil && *rate.Direction != direction || modality != "" && rate.Modality != nil && *rate.Modality != modality {
		return "unknown"
	}
	return component
}
func CatalogProviderForwardingOperation(operation map[string]any) bool {
	conditions, ok := operation["conditions"].([]any)
	if !ok || len(conditions) != 1 {
		return false
	}
	condition, ok := conditions[0].(map[string]any)
	if !ok || condition["mode"] != "full" || condition["pass_missing_key"] == true {
		return false
	}
	switch operation["mode"] {
	case "move":
		return operation["from"] == "extra_body.provider" && operation["to"] == "provider" &&
			condition["path"] == "extra_body.provider" && condition["value"] == nil && condition["invert"] == true
	case "delete":
		empty, ok := condition["value"].(map[string]any)
		return operation["path"] == "extra_body" && condition["path"] == "extra_body" &&
			condition["invert"] != true && ok && len(empty) == 0
	default:
		return false
	}
}

// A catalog is a projection, not request admission. Use the same override engine
// as relay; apply provider selection to the resulting routing preferences.
func (pricingPolicy *Projection) CatalogProviderAllowed(route UpstreamCatalogRoute, apiModel, group string, user PricingUserContext, offer SourceOffer) (bool, bool, bool) {
	if route.ParamOverride == nil || strings.TrimSpace(*route.ParamOverride) == "" || strings.TrimSpace(*route.ParamOverride) == "{}" {
		return true, true, CatalogServiceTierAllowed(pricingPolicy.runtime.Capabilities, route, offer, "", nil, nil)
	}
	var overrides map[string]any
	if pricingPolicy.runtime.Capabilities.Codec.UnmarshalJsonStr(*route.ParamOverride, &overrides) != nil {
		return false, true, false
	}

	if operations, exists := overrides["operations"]; exists {
		entries, ok := operations.([]any)
		if !ok {
			return true, false, false
		}
		for _, entry := range entries {
			operation, ok := entry.(map[string]any)
			if !ok {
				return true, false, false
			}

			if CatalogProviderForwardingOperation(operation) {
				continue
			}
			if operation["mode"] != "set" && operation["mode"] != "delete" {
				return true, false, false
			}
			paths := []string{}
			switch conditions := operation["conditions"].(type) {
			case nil:
			case map[string]any:
				for path := range conditions {
					paths = append(paths, path)
				}
			case []any:
				for _, raw := range conditions {
					condition, ok := raw.(map[string]any)
					if !ok {
						return true, false, false
					}
					path, _ := condition["path"].(string)
					paths = append(paths, path)
				}
			default:
				return true, false, false
			}
			for _, path := range paths {
				switch strings.TrimSpace(path) {
				case "user_id":
					if user.Id <= 0 {
						return true, false, false
					}
				case "user_group":
					if user.Group == "" {
						return true, false, false
					}
				case "using_group", "model", "upstream_model", "original_model", "request_path":
				default:
					return true, false, false
				}
			}
		}
	}

	body, err := pricingPolicy.runtime.Capabilities.Codec.Marshal(map[string]string{"model": route.ModelID})
	if err != nil {
		return false, true, false
	}
	path, _ := pricingPolicy.runtime.Capabilities.Endpoints.GetDefaultEndpointInfo(route.Endpoint)
	if route.Endpoint == PricingEndpointSystemOne {
		path = PricingEndpointInfo{Path: "/v1/systemone", Method: "POST"}
	}
	body, err = pricingPolicy.runtime.ApplyOverride(body, overrides, OverrideContext{UserID: user.Id, UserGroup: user.Group, UsingGroup: group, Model: apiModel, Path: path.Path, Upstream: route.ModelID})
	if err != nil {
		return false, true, false
	}
	var fields struct {
		ServiceTier string `json:"service_tier"`
		Provider    struct {
			Only, Ignore, Order []string
			AllowFallbacks      *bool `json:"allow_fallbacks"`
		} `json:"provider"`
	}
	if pricingPolicy.runtime.Capabilities.Codec.Unmarshal(body, &fields) != nil {
		return false, true, false
	}
	var routing struct {
		Model    string                     `json:"model"`
		Provider map[string]json.RawMessage `json:"provider"`
	}
	if pricingPolicy.runtime.Capabilities.Codec.Unmarshal(body, &routing) != nil {
		return false, true, false
	}
	certain := routing.Model == route.ModelID
	for key, value := range routing.Provider {
		switch key {
		case "only", "ignore", "order", "allow_fallbacks", "sort":
		default:
			if string(value) != "null" {
				certain = false
			}
		}
	}
	preferences := fields.Provider
	tierAllowed := CatalogServiceTierAllowed(pricingPolicy.runtime.Capabilities, route, offer, fields.ServiceTier, preferences.Only, preferences.Order)
	if len(preferences.Only) == 0 && len(preferences.Ignore) == 0 && (preferences.AllowFallbacks == nil || *preferences.AllowFallbacks || len(preferences.Order) == 0) {
		return true, certain, tierAllowed
	}
	if offer.Provider == nil {
		return false, true, false
	}
	tag := ""
	if offer.Provider.Tag != nil {
		tag = *offer.Provider.Tag
	} else if offer.Provider.ID != nil {
		tag = *offer.Provider.ID
	}
	matches := func(slug string) bool {
		candidate := strings.ToLower(tag)
		if base, ok := strings.CutSuffix(candidate, "/priority"); ok {
			candidate = base + "/fast"
		}
		slug = strings.ToLower(slug)
		return candidate == slug || strings.HasPrefix(candidate, slug+"/")
	}
	if len(preferences.Only) > 0 && !slices.ContainsFunc(preferences.Only, matches) || slices.ContainsFunc(preferences.Ignore, matches) {
		return false, true, false
	}
	return preferences.AllowFallbacks == nil || *preferences.AllowFallbacks || len(preferences.Order) == 0 || slices.ContainsFunc(preferences.Order, matches), certain, tierAllowed
}
func (pricingPolicy *Projection) UpstreamProviderOffers(item Pricing, group string, ratio float64, validRatio bool, user PricingUserContext) []ConsumerProviderOffer {
	result := []ConsumerProviderOffer{}
	seen := map[string]int{}
	for offer := range pricingPolicy.IterateUpstreamProviderOffers(item, group, ratio, validRatio, user) {
		if previous, exists := seen[offer.ID]; exists {
			if result[previous].Availability == "unknown" && offer.Availability == "eligible" || result[previous].Availability == offer.Availability && !result[previous].MinimumEligible && offer.MinimumEligible {
				result[previous] = offer
			}
			continue
		}
		seen[offer.ID] = len(result)
		result = append(result, offer)
	}
	slices.SortFunc(result, func(a, b ConsumerProviderOffer) int { return strings.Compare(a.ID, b.ID) })
	return result
}
func (pricingPolicy *Projection) IterateUpstreamProviderOffers(item Pricing, group string, ratio float64, validRatio bool, user PricingUserContext) iter.Seq[ConsumerProviderOffer] {
	return func(yield func(ConsumerProviderOffer) bool) {
		for _, route := range item.UpstreamCatalogRoutesForGroup(group) {
			if route.Document == nil {
				continue
			}
			for offerIndex, offer := range route.Document.Offers {
				mode, knownMode := CatalogOfferMode(pricingPolicy.runtime.Capabilities, offer, route.ModelID)
				if !knownMode || mode != route.Mode || offer.Operation != nil && *offer.Operation != route.Operation {
					continue
				}
				allowed, known, tierAllowed := pricingPolicy.CatalogProviderAllowed(route, item.ModelName, group, user, offer)
				if !allowed {
					continue
				}
				certain := known && offer.Eligibility != nil
				id := CatalogHash([]byte(route.DocumentHash + "\n" + route.ModelID + "\n" + strconv.Itoa(offerIndex) + "\n" + route.Operation + "\n" + route.Mode))
				row := ConsumerProviderOffer{MinimumEligible: tierAllowed, DocumentHash: route.DocumentHash, ID: id, UpstreamModel: route.ModelID, Operation: route.Operation, Mode: route.Mode, Scope: offer.Scope, Provider: offer.Provider, Variant: offer.Variant, Prices: []ConsumerProviderPrice{}, Coverage: offer.Coverage, Eligibility: offer.Eligibility, Capabilities: offer.Capabilities, Limits: offer.Limits}
				row.Limitations = slices.Clone(offer.Limitations)
				row.Availability = "eligible"
				if offer.Eligibility != nil && !offer.Eligibility.Eligible {
					row.Availability = "ineligible"
				}
				if !certain {
					row.Availability = "unknown"
					row.Limitations = append(row.Limitations, "request_dependent_provider_eligibility")
				}
				for _, rate := range offer.Components {
					price := ConsumerProviderPrice{UpstreamRate: UpstreamRate(rate), Component: CatalogComponentName(rate)}
					if validRatio && ratio >= 0 && !math.IsNaN(ratio) && !math.IsInf(ratio, 0) && rate.AmountUSD != nil && rate.Normalization.Comparable && rate.Normalization.Effective && rate.Unit != "" && rate.Unit != "unknown" && rate.Unit != "source_defined" {
						usd, parseErr := decimal.NewFromString(*rate.AmountUSD)
						if parseErr == nil && !usd.IsNegative() {
							rub := usd.Mul(decimal.NewFromInt(100)).Mul(decimal.NewFromFloat(ratio)).String()
							price.AmountRub = &rub
						}
					}
					row.Prices = append(row.Prices, price)
				}
				row.Prices = CatalogDetailPrices(pricingPolicy.runtime.Capabilities, row.Prices)
				if !yield(row) {
					return
				}
			}
		}
	}
}

// CatalogShadowedModelPrices marks model-page prices that the generic endpoint
// schedule replaces for the same component and unit. Public minima and schedule
// tiers (CatalogSchedulePrices) ignore them, so offer details must not show them
// either.
func CatalogShadowedModelPrices(deps Capabilities, prices []ConsumerProviderPrice) []bool {
	sources := make([]string, len(prices))
	generic := map[string]bool{}
	for index, price := range prices {
		var provenance struct {
			Source string `json:"source"`
		}
		if deps.Codec.Unmarshal(price.Provenance, &provenance) != nil {
			continue
		}
		sources[index] = provenance.Source
		if provenance.Source == "generic" && price.Component != "unknown" {
			generic[price.Component+"/"+price.Unit] = true
		}
	}
	shadowed := make([]bool, len(prices))
	for index, price := range prices {
		shadowed[index] = sources[index] == "model_page" && price.Component != "unknown" && generic[price.Component+"/"+price.Unit]
	}
	return shadowed
}

func CatalogDetailPrices(deps Capabilities, prices []ConsumerProviderPrice) []ConsumerProviderPrice {
	shadowed := CatalogShadowedModelPrices(deps, prices)
	result := make([]ConsumerProviderPrice, 0, len(prices))
	for index, price := range prices {
		if shadowed[index] {
			continue
		}
		duplicate := slices.IndexFunc(result, func(kept ConsumerProviderPrice) bool { return CatalogSamePrice(deps, kept, price) })
		if duplicate < 0 {
			result = append(result, price)
			continue
		}
		if CatalogPreferredDuplicate(deps, price, result[duplicate]) {
			result[duplicate] = price
		}
	}
	return result
}

// CatalogPreferredDuplicate keeps the copy of an identical price that schedules
// and minima can use: a rouble amount first, then readable provenance.
func CatalogPreferredDuplicate(deps Capabilities, candidate, kept ConsumerProviderPrice) bool {
	if (candidate.AmountRub != nil) != (kept.AmountRub != nil) {
		return candidate.AmountRub != nil
	}
	var provenance struct {
		Source string `json:"source"`
	}
	candidateReadable := deps.Codec.Unmarshal(candidate.Provenance, &provenance) == nil
	keptReadable := deps.Codec.Unmarshal(kept.Provenance, &provenance) == nil
	return candidateReadable && !keptReadable
}

func CatalogPricedRate(price ConsumerProviderPrice) bool {
	return price.AmountUSD != nil && price.Unit != "" && price.Unit != "unknown" && price.Unit != "source_defined"
}

// CatalogSamePriceSlot reports whether two rates bill the same component, unit,
// quantity, conditions and variant, regardless of amount and source labels.
func CatalogSamePriceSlot(deps Capabilities, a, b ConsumerProviderPrice) bool {
	if a.Component != b.Component || a.Unit != b.Unit || a.Component == "unknown" && a.ComponentID != b.ComponentID {
		return false
	}
	quantityA, errA := decimal.NewFromString(a.Quantity)
	quantityB, errB := decimal.NewFromString(b.Quantity)
	if errA != nil || errB != nil || !quantityA.Equal(quantityB) {
		return false
	}
	for _, pair := range [][2]json.RawMessage{{a.Conditions, b.Conditions}, {a.Variant, b.Variant}} {
		var left, right map[string]any
		if len(pair[0]) > 0 && deps.Codec.Unmarshal(pair[0], &left) != nil || len(pair[1]) > 0 && deps.Codec.Unmarshal(pair[1], &right) != nil {
			return false
		}
		if len(left) == 0 || len(right) == 0 {
			if len(left) != len(right) {
				return false
			}
			continue
		}
		leftRaw, errL := deps.Codec.Marshal(left)
		rightRaw, errR := deps.Codec.Marshal(right)
		if errL != nil || errR != nil || string(leftRaw) != string(rightRaw) {
			return false
		}
	}
	return true
}

func CatalogSamePrice(deps Capabilities, a, b ConsumerProviderPrice) bool {
	if !CatalogPricedRate(a) || !CatalogPricedRate(b) || !CatalogSamePriceSlot(deps, a, b) {
		return false
	}
	amountA, errA := decimal.NewFromString(*a.AmountUSD)
	amountB, errB := decimal.NewFromString(*b.AmountUSD)
	return errA == nil && errB == nil && amountA.Equal(amountB)
}

// CatalogProviderDisplayOffers folds a model-scope offer into the provider
// (endpoint-scope) offers of the same catalog document, upstream model,
// operation and mode. Its priced rates fill only price slots a provider offer
// lacks and its capabilities fill only keys a provider offer does not declare.
// The model-scope offer stays separate when that group has no provider offer,
// when it holds a priced rate that cannot be carried, or when one of its prices
// contradicts a provider price in the same slot.
func CatalogProviderDisplayOffers(deps Capabilities, offers []ConsumerProviderOffer) []ConsumerProviderOffer {
	type key struct{ document, model, operation, mode string }
	endpoints := map[key][]int{}
	result := make([]ConsumerProviderOffer, 0, len(offers))
	for _, offer := range offers {
		if offer.Scope == "endpoint" {
			offer.Prices = slices.Clone(offer.Prices)
			k := key{offer.DocumentHash, offer.UpstreamModel, offer.Operation, offer.Mode}
			endpoints[k] = append(endpoints[k], len(result))
		}
		result = append(result, offer)
	}
	folded := make([]bool, len(result))
	for index, offer := range result {
		targets := endpoints[key{offer.DocumentHash, offer.UpstreamModel, offer.Operation, offer.Mode}]
		if offer.Scope != "model" || len(targets) == 0 {
			continue
		}
		foldable := true
		for _, price := range offer.Prices {
			if price.AmountUSD == nil {
				continue
			}
			if !CatalogPricedRate(price) {
				foldable = false
				break
			}
			for _, target := range targets {
				if slices.ContainsFunc(result[target].Prices, func(kept ConsumerProviderPrice) bool {
					return CatalogPricedRate(kept) && CatalogSamePriceSlot(deps, kept, price) && !CatalogSamePrice(deps, kept, price)
				}) {
					foldable = false
					break
				}
			}
			if !foldable {
				break
			}
		}
		if !foldable {
			continue
		}
		folded[index] = true
		var capabilities map[string]json.RawMessage
		if len(offer.Capabilities) > 0 && deps.Codec.Unmarshal(offer.Capabilities, &capabilities) != nil {
			capabilities = nil
		}
		for _, target := range targets {
			endpoint := &result[target]
			for _, price := range offer.Prices {
				if CatalogPricedRate(price) && !slices.ContainsFunc(endpoint.Prices, func(kept ConsumerProviderPrice) bool { return CatalogSamePrice(deps, kept, price) }) {
					endpoint.Prices = append(endpoint.Prices, price)
				}
			}
			if len(capabilities) == 0 {
				continue
			}
			merged := map[string]json.RawMessage{}
			if len(endpoint.Capabilities) > 0 && deps.Codec.Unmarshal(endpoint.Capabilities, &merged) != nil {
				continue
			}
			if merged == nil {
				merged = map[string]json.RawMessage{}
			}
			added := false
			for name, value := range capabilities {
				if _, exists := merged[name]; !exists {
					merged[name] = value
					added = true
				}
			}
			if !added {
				continue
			}
			if raw, err := deps.Codec.Marshal(merged); err == nil {
				endpoint.Capabilities = raw
			}
		}
	}
	kept := make([]ConsumerProviderOffer, 0, len(result))
	for index, offer := range result {
		if !folded[index] {
			kept = append(kept, offer)
		}
	}
	return kept
}

func CatalogMinima(deps Capabilities, offers []ConsumerProviderOffer) []ConsumerPricingOperation {
	return CatalogMinimumOperations(deps, slices.Values(offers))
}
func CatalogMinimumOperations(deps Capabilities, offers iter.Seq[ConsumerProviderOffer]) []ConsumerPricingOperation {
	type key struct{ operation, mode string }
	schedules := map[key][]CatalogScheduledPrice{}
	seen := map[string]bool{}
	minima := CatalogUnconditionalMinima(deps, func(yield func(ConsumerProviderOffer) bool) {
		for offer := range offers {
			if offer.MinimumEligible && offer.Availability == "eligible" && (offer.Eligibility == nil || offer.Eligibility.Eligible) {
				if offer.ID != "" && seen[offer.ID] {
					continue
				}
				seen[offer.ID] = true
				k := key{offer.Operation, offer.Mode}
				schedules[k] = append(schedules[k], CatalogScheduledRates(deps, offer)...)
			}
			if !yield(offer) {
				return
			}
		}
	})
	for index := range minima {
		minima[index].Tiers = CatalogScheduleTiers(deps, schedules[key{minima[index].Operation, minima[index].Mode}])
		if len(minima[index].Tiers) == 0 {
			minima[index].Prices = []ConsumerUnitPrice{}
			from := false
			minima[index].IsFrom = &from
		}
	}
	return minima
}
func CatalogUnconditionalMinima(deps Capabilities, offers iter.Seq[ConsumerProviderOffer]) []ConsumerPricingOperation {
	type key struct{ operation, mode string }
	type winner struct {
		Price           ConsumerProviderPrice `json:"-"`
		value, quantity decimal.Decimal
	}
	grouped := map[key]map[string]winner{}
	// A minimum is a "from" price only when it was chosen among distinct unit
	// amounts (variants, providers, schedules), not merely because it is upstream.
	candidates := map[key]map[string]map[string]bool{}
	for offer := range offers {
		if !offer.MinimumEligible || offer.Availability != "eligible" || offer.Eligibility != nil && !offer.Eligibility.Eligible {
			continue
		}
		k := key{offer.Operation, offer.Mode}
		if grouped[k] == nil {
			grouped[k] = map[string]winner{}
			candidates[k] = map[string]map[string]bool{}
		}
		for _, price := range CatalogSchedulePrices(deps, offer) {
			if price.AmountRub == nil {
				continue
			}
			value, err := decimal.NewFromString(*price.AmountRub)
			if err != nil || value.IsNegative() {
				continue
			}
			quantity, err := decimal.NewFromString(price.Quantity)
			if err != nil || !quantity.IsPositive() {
				continue
			}
			slot := price.Component + "/" + price.Unit
			if candidates[k][slot] == nil {
				candidates[k][slot] = map[string]bool{}
			}
			candidates[k][slot][value.DivRound(quantity, 40).String()] = true
			old, exists := grouped[k][slot]
			if !exists || value.Mul(old.quantity).LessThan(old.value.Mul(quantity)) {
				grouped[k][slot] = winner{price, value, quantity}
			}
		}
	}
	keys := make([]key, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b key) int {
		if a.operation != b.operation {
			return strings.Compare(a.operation, b.operation)
		}
		return strings.Compare(a.mode, b.mode)
	})
	result := []ConsumerPricingOperation{}
	for _, k := range keys {
		row := ConsumerPricingOperation{Operation: k.operation, Mode: k.mode, Prices: []ConsumerUnitPrice{}}
		from := false
		row.IsFrom = &from
		for slot, selected := range grouped[k] {
			precision := max(int32(32), -selected.value.Exponent()+int32(selected.quantity.NumDigits())+32)
			value := selected.value.DivRound(selected.quantity, precision)
			converted, _ := value.Float64()
			if math.IsNaN(converted) || math.IsInf(converted, 0) || converted == 0 && !selected.value.IsZero() {
				continue
			}
			amount := converted
			row.Prices = append(row.Prices, ConsumerUnitPrice{Component: selected.Price.Component, Unit: selected.Price.Unit, AmountRub: &amount})
			from = from || len(candidates[k][slot]) > 1
		}
		slices.SortFunc(row.Prices, func(a, b ConsumerUnitPrice) int {
			if a.Component != b.Component {
				return strings.Compare(a.Component, b.Component)
			}
			return strings.Compare(a.Unit, b.Unit)
		})
		result = append(result, row)
	}
	return result
}
func MergeCatalogMinima(existing, upstream []ConsumerPricingOperation) []ConsumerPricingOperation {
	for _, addition := range upstream {
		index := slices.IndexFunc(existing, func(row ConsumerPricingOperation) bool {
			return row.Operation == addition.Operation && row.Mode == addition.Mode
		})
		if index < 0 {
			existing = append(existing, addition)
			continue
		}
		row := &existing[index]
		row.Tiers = append(slices.Clone(row.Tiers), addition.Tiers...)
		from := row.IsFrom != nil && *row.IsFrom || addition.IsFrom != nil && *addition.IsFrom
		row.Prices = slices.Clone(row.Prices)
		for _, price := range addition.Prices {
			slot := slices.IndexFunc(row.Prices, func(old ConsumerUnitPrice) bool { return old.Component == price.Component && old.Unit == price.Unit })
			if slot < 0 {
				row.Prices = append(row.Prices, price)
				continue
			}
			old := row.Prices[slot].AmountRub
			from = from || old != nil && price.AmountRub != nil && *old != *price.AmountRub
			if old == nil || price.AmountRub != nil && *price.AmountRub < *old {
				row.Prices[slot] = price
			}
		}
		row.IsFrom = &from
	}
	slices.SortStableFunc(existing, func(a, b ConsumerPricingOperation) int {
		if a.Operation != b.Operation {
			return strings.Compare(a.Operation, b.Operation)
		}
		return strings.Compare(a.Mode, b.Mode)
	})
	return existing
}
func CatalogServiceTier(deps Capabilities, variant []byte) string {
	var fields struct {
		Tier string `json:"service_tier"`
	}
	if len(variant) != 0 && deps.Codec.Unmarshal(variant, &fields) != nil {
		return "unknown"
	}
	if fields.Tier == "" || fields.Tier == "standard" {
		return "default"
	}
	if fields.Tier == "fast" {
		return "priority"
	}
	return fields.Tier
}
func CatalogServiceTierAllowed(deps Capabilities, route UpstreamCatalogRoute, offer SourceOffer, requested string, only, order []string) bool {
	tier := CatalogServiceTier(deps, offer.Variant)
	if tier == "unknown" {
		return false
	}
	if tier == "default" {
		return true
	}
	if offer.Provider != nil && offer.Provider.Tag != nil {
		tag := *offer.Provider.Tag
		if slices.Contains(only, tag) || slices.Contains(order, tag) {
			return true
		}
	}
	if requested == "fast" {
		requested = "priority"
	}
	if requested != "" {
		return tier == requested
	}
	if len(order) == 0 && (tier == "flex" && strings.HasSuffix(route.ModelID, ":floor") || tier == "priority" && strings.HasSuffix(route.ModelID, ":nitro")) {
		return true
	}

	return (tier == "flex" || tier == "priority" || tier == "ultrafast") && route.ServiceTierEnabled
}

// A component variant (resolution, language, quality, operation...) identifies a
// separately billed SKU. Prices of different variants are never alternatives for
// one request, so each variant keeps its own public condition.
func CatalogPriceVariant(deps Capabilities, raw json.RawMessage) (string, []PublicPricingCondition, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, true
	}
	var fields map[string]json.RawMessage
	if deps.Codec.Unmarshal(raw, &fields) != nil || fields == nil {
		return "", nil, false
	}
	var key strings.Builder
	conditions := []PublicPricingCondition{}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		var value string
		if deps.Codec.Unmarshal(fields[name], &value) != nil {
			var scalar any
			if deps.Codec.Unmarshal(fields[name], &scalar) != nil {
				return "", nil, false
			}
			switch scalar.(type) {
			case bool, float64:
				value = strings.TrimSpace(string(fields[name]))
			default:
				return "", nil, false
			}
		}
		key.WriteString(name + "=" + value + "\n")
		conditions = append(conditions, PublicPricingCondition{Kind: "comparison", Subject: "usage", Argument: name, Operator: "==", StringValue: &value})
	}
	return key.String(), conditions, true
}
func CatalogSchedulePrices(deps Capabilities, offer ConsumerProviderOffer) []ConsumerProviderPrice {
	shadowed := CatalogShadowedModelPrices(deps, offer.Prices)
	result := []ConsumerProviderPrice{}
	for index, price := range offer.Prices {
		if price.Component == "unknown" || price.AmountRub == nil || shadowed[index] {
			continue
		}
		var provenance struct {
			Source string `json:"source"`
		}
		if deps.Codec.Unmarshal(price.Provenance, &provenance) != nil {
			continue
		}
		if _, valid := CatalogRateWindow(deps, price); !valid {
			continue
		}
		if _, _, valid := CatalogPriceVariant(deps, price.Variant); !valid {
			continue
		}
		result = append(result, price)
	}
	return result
}
func CatalogRateWindow(deps Capabilities, price ConsumerProviderPrice) (CatalogPriceWindow, bool) {
	window := CatalogPriceWindow{}
	if len(price.Conditions) == 0 || string(price.Conditions) == "null" {
		return window, true
	}
	var fields map[string]any
	if deps.Codec.Unmarshal(price.Conditions, &fields) != nil || fields == nil {
		return window, false
	}
	for key, value := range fields {
		switch key {
		case "contextMin", "contextMax", "utcDay", "utcStart", "utcEnd", "appliedOverrideIndexes":
		case "chargeRelationship":
			if value != "minimum" {
				return window, false
			}
		default:
			return window, false
		}
	}
	if deps.Codec.Unmarshal(price.Conditions, &window) != nil {
		return window, false
	}
	if window.ContextMin != nil && (*window.ContextMin < 0 || *window.ContextMin > CatalogUnboundedContext) || window.ContextMax != nil && (*window.ContextMax < 0 || *window.ContextMax > CatalogUnboundedContext) {
		return window, false
	}
	if window.ContextMin != nil && window.ContextMax != nil && *window.ContextMax < *window.ContextMin || window.Day != nil && (*window.Day < 0 || *window.Day > 6) {
		return window, false
	}
	start, end := 0, 2400
	if window.Start != nil {
		start = *window.Start
	}
	if window.End != nil {
		end = *window.End
	}
	return window, start >= 0 && start < end && end <= 2400 && start%100 < 60 && end%100 < 60
}
func CatalogScheduleTiers(deps Capabilities, rates []CatalogScheduledPrice) []PublicPricingTier {
	type group struct{ tier, variant string }
	byGroup := map[group][]CatalogScheduledPrice{}
	serviceTiers := []string{}
	for _, rate := range rates {
		k := group{rate.Tier, rate.Variant}
		byGroup[k] = append(byGroup[k], rate)
		if !slices.Contains(serviceTiers, rate.Tier) {
			serviceTiers = append(serviceTiers, rate.Tier)
		}
	}
	tiers := []PublicPricingTier{}
	groups := slices.Collect(maps.Keys(byGroup))
	slices.SortFunc(groups, func(a, b group) int {
		if a.tier != b.tier {
			return strings.Compare(a.tier, b.tier)
		}
		return strings.Compare(a.variant, b.variant)
	})
	for _, k := range groups {
		tier, entries := k.tier, byGroup[k]
		contexts, times := []int64{}, []int{}
		for _, rate := range entries {
			contexts = append(contexts, rate.Low, rate.High+1)
			times = append(times, rate.Start, rate.End)
		}
		slices.Sort(contexts)
		contexts = slices.Compact(contexts)
		slices.Sort(times)
		times = slices.Compact(times)

		if len(contexts)*len(times)*7 > CatalogMaxScheduleCells {
			return nil
		}
		for contextIndex := 0; contextIndex+1 < len(contexts); contextIndex++ {
			low, high := contexts[contextIndex], contexts[contextIndex+1]-1
			for timeIndex := 0; timeIndex+1 < len(times); timeIndex++ {
				start, end := times[timeIndex], times[timeIndex+1]
				byPrices := map[string][]int{}
				pricesByKey := map[string][]ConsumerUnitPrice{}
				for day := range 7 {
					candidates := ConsumerProviderOffer{Availability: "eligible", MinimumEligible: true, Prices: []ConsumerProviderPrice{}}
					for _, rate := range entries {
						if rate.Low <= low && rate.High >= high && rate.Start <= start && rate.End >= end && (rate.Day == -1 || rate.Day == day) {
							candidates.Prices = append(candidates.Prices, rate.Price)
						}
					}

					minimum := CatalogUnconditionalMinima(deps, slices.Values([]ConsumerProviderOffer{candidates}))
					if len(minimum) == 0 || len(minimum[0].Prices) == 0 {
						continue
					}
					prices := minimum[0].Prices
					wire, err := deps.Codec.Marshal(prices)
					if err != nil {
						continue
					}
					key := string(wire)
					byPrices[key] = append(byPrices[key], day)
					pricesByKey[key] = prices
				}
				keys := make([]string, 0, len(byPrices))
				for key := range byPrices {
					keys = append(keys, key)
				}
				slices.Sort(keys)
				for _, key := range keys {
					conditions := []PublicPricingCondition{}
					if tier != "default" || len(serviceTiers) > 1 {
						value := tier
						conditions = append(conditions, PublicPricingCondition{Kind: "comparison", Subject: "request", Argument: "service_tier", Operator: "==", StringValue: &value})
					}
					conditions = append(conditions, entries[0].Conditions...)
					if low > 0 {
						value := float64(low)
						conditions = append(conditions, PublicPricingCondition{Kind: "comparison", Subject: "context", Argument: "tokens", Unit: "token", Operator: ">=", Value: &value})
					}
					if high != CatalogUnboundedContext {
						value := float64(high)
						conditions = append(conditions, PublicPricingCondition{Kind: "comparison", Subject: "context", Argument: "tokens", Unit: "token", Operator: "<=", Value: &value})
					}
					if start != 0 {
						value := float64(start)
						conditions = append(conditions, PublicPricingCondition{Kind: "comparison", Subject: "time", Argument: "utc_hhmm", Operator: ">=", Value: &value})
					}
					if end != 2400 {
						value := float64(end)
						conditions = append(conditions, PublicPricingCondition{Kind: "comparison", Subject: "time", Argument: "utc_hhmm", Operator: "<", Value: &value})
					}
					if len(byPrices[key]) != 7 {
						days := PublicPricingCondition{Kind: "any_of"}
						for _, day := range byPrices[key] {
							value := float64(day)
							days.Conditions = append(days.Conditions, PublicPricingCondition{Kind: "comparison", Subject: "time", Argument: "utc_day", Operator: "==", Value: &value})
						}
						conditions = append(conditions, days)
					}
					row := PublicPricingTier{Prices: pricesByKey[key]}
					if len(conditions) == 1 {
						row.Condition = &conditions[0]
					} else if len(conditions) > 1 {
						row.Condition = &PublicPricingCondition{Kind: "all_of", Conditions: conditions}
					}
					tiers = append(tiers, row)
				}
			}
		}
	}
	return tiers
}
func CatalogScheduledRates(deps Capabilities, offer ConsumerProviderOffer) []CatalogScheduledPrice {
	result := []CatalogScheduledPrice{}
	for _, price := range CatalogSchedulePrices(deps, offer) {
		window, _ := CatalogRateWindow(deps, price)
		variant, conditions, _ := CatalogPriceVariant(deps, price.Variant)
		rate := CatalogScheduledPrice{Price: price, Tier: CatalogServiceTier(deps, offer.Variant), Variant: variant, Conditions: conditions, High: CatalogUnboundedContext, Day: -1, End: 2400}
		if window.ContextMin != nil {
			rate.Low = *window.ContextMin
		}
		if window.ContextMax != nil {
			rate.High = *window.ContextMax
		}
		if window.Day != nil {
			rate.Day = *window.Day
		}
		if window.Start != nil {
			rate.Start = *window.Start
		}
		if window.End != nil {
			rate.End = *window.End
		}
		result = append(result, rate)
	}
	return result
}
func (g *EffectiveGroupPricing) SetTiers(tiers []EffectivePricingTier) {
	g.Tiers = tiers
	g.IsFree = len(tiers) > 0
	for _, tier := range tiers {
		for _, price := range tier.UnitPrices {
			if price.AmountRub != 0 {
				g.IsFree = false
			}
		}
	}
}

// tokenExpressionTiers projects a token expression onto ready-to-display tiers.
// A component is listed iff some tier of the expression references it, so a
// zero amount means "free" while absence means "not priced". Any tier whose
// body has no linear price makes the whole projection empty: a partial table
// would be a wrong price.
func TokenExpressionTiers(deps Capabilities, expression string, outputToRub float64) []EffectivePricingTier {
	tiers, err := deps.Expressions.EnumerateTiers(expression)
	if err != nil {
		return []EffectivePricingTier{}
	}
	referenced := make(map[string]bool)
	for _, tier := range tiers {
		if !tier.Linear {
			return []EffectivePricingTier{}
		}
		for variable := range tier.Coefficients {
			referenced[variable] = true
		}
	}
	variables := OrderedComponentVariables(referenced)

	result := make([]EffectivePricingTier, 0, len(tiers))
	for _, tier := range tiers {
		prices := make([]EffectiveUnitPrice, 0, len(variables))
		for _, variable := range variables {
			prices = append(prices, EffectiveUnitPrice{
				Component: ComponentName(variable),
				Unit:      "million_tokens",
				AmountRub: tier.Coefficients[variable] * outputToRub * 1_000_000,
			})
		}
		result = append(result, EffectivePricingTier{Label: tier.Label, Condition: ClassifyTierConditions(tier.Conditions), UnitPrices: prices})
	}
	return result
}
func OrderedComponentVariables(referenced map[string]bool) []string {
	ordered := make([]string, 0, len(referenced))
	rest := make([]string, 0)
	for _, entry := range EffectivePricingComponents {
		if referenced[entry.variable] {
			ordered = append(ordered, entry.variable)
		}
	}
	for variable := range referenced {
		if ComponentName(variable) == variable {
			rest = append(rest, variable)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}
func ComponentName(variable string) string {
	for _, entry := range EffectivePricingComponents {
		if entry.variable == variable {
			return entry.component
		}
	}
	return variable
}
func ClassifyTierConditions(conditions []ExpressionCondition) *EffectivePricingTierCondition {
	if len(conditions) == 0 {
		return nil
	}
	if bounds, ok := TokenBoundsCondition(conditions); ok {
		return bounds
	}
	if windows, ok := TimeOfDayCondition(conditions); ok {
		return windows
	}
	return &EffectivePricingTierCondition{Kind: "other"}
}

// tokenBoundsCondition folds a chain of comparisons on one token variable into
// (min, max]. Token counts are integers, so strict comparisons shift by one.
func TokenBoundsCondition(conditions []ExpressionCondition) (*EffectivePricingTierCondition, bool) {
	var comparisons []ExpressionCondition
	for _, condition := range conditions {
		if !FlattenConjunction(condition, &comparisons) {
			return nil, false
		}
	}
	result := &EffectivePricingTierCondition{Unit: "token"}
	variable := comparisons[0].Var
	for _, comparison := range comparisons {
		if comparison.Var != variable || comparison.Arg != "" || TierConditionKinds[variable] == "" {
			return nil, false
		}
		op := comparison.Op
		if comparison.Negated {
			op = NegateComparison(op)
		}
		value := comparison.Value
		if (op == "<" || op == ">=") && value == math.Trunc(value) {
			value--
		}
		switch op {
		case "<", "<=":
			if result.MaxValue == nil || value < *result.MaxValue {
				result.MaxValue = &value
			}
		case ">", ">=":
			if result.MinValue == nil || value > *result.MinValue {
				result.MinValue = &value
			}
		default:
			return nil, false
		}
	}
	result.Kind = TierConditionKinds[variable]
	return result, true
}
func FlattenConjunction(condition ExpressionCondition, comparisons *[]ExpressionCondition) bool {
	if len(condition.Operands) == 0 {
		*comparisons = append(*comparisons, condition)
		return true
	}
	if condition.Op != "&&" || condition.Negated {
		return false
	}
	for _, operand := range condition.Operands {
		if !FlattenConjunction(operand, comparisons) {
			return false
		}
	}
	return true
}
func NegateComparison(op string) string {
	switch op {
	case "<":
		return ">="
	case "<=":
		return ">"
	case ">":
		return "<="
	case ">=":
		return "<"
	case "==":
		return "!="
	case "!=":
		return "=="
	default:
		return op
	}
}

// timeOfDayCondition evaluates hour("<zone>") guards for each hour of the day
// and reports the hours in which the tier applies as [start, end) windows, so
// an else branch yields the exact complement of its sibling.
func TimeOfDayCondition(conditions []ExpressionCondition) (*EffectivePricingTierCondition, bool) {
	zone := ""
	hours := uint32(1<<HoursPerDay - 1)
	for _, condition := range conditions {
		mask, ok := HourMask(condition, &zone)
		if !ok {
			return nil, false
		}
		hours &= mask
	}
	result := &EffectivePricingTierCondition{Kind: "time_of_day", TimeZone: zone, TimeWindows: []EffectivePricingTimeWindow{}}
	for hour := 0; hour < HoursPerDay; hour++ {
		if hours&(1<<hour) == 0 {
			continue
		}
		end := hour + 1
		for end < HoursPerDay && hours&(1<<end) != 0 {
			end++
		}
		result.TimeWindows = append(result.TimeWindows, EffectivePricingTimeWindow{StartHour: hour, EndHour: end})
		hour = end
	}
	return result, true
}
func HourMask(condition ExpressionCondition, zone *string) (uint32, bool) {
	const allHours = uint32(1<<HoursPerDay - 1)
	var mask uint32
	switch condition.Op {
	case "&&", "||":
		mask = allHours
		if condition.Op == "||" {
			mask = 0
		}
		for _, operand := range condition.Operands {
			operandMask, ok := HourMask(operand, zone)
			if !ok {
				return 0, false
			}
			if condition.Op == "&&" {
				mask &= operandMask
			} else {
				mask |= operandMask
			}
		}
	default:
		if condition.Var != "hour" || (*zone != "" && condition.Arg != *zone) {
			return 0, false
		}
		*zone = condition.Arg
		for hour := 0; hour < HoursPerDay; hour++ {
			if CompareNumbers(float64(hour), condition.Op, condition.Value) {
				mask |= 1 << hour
			}
		}
	}
	if condition.Negated {
		mask ^= allHours
	}
	return mask, true
}
func CompareNumbers(left float64, op string, right float64) bool {
	switch op {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "==":
		return left == right
	case "!=":
		return left != right
	default:
		return false
	}
}
func ConsumerPricingRevision(deps Capabilities, source string, context EffectivePricingPageContext) (string, error) {
	value, err := deps.Codec.Marshal(struct {
		Source, UserGroup, UsingGroup string
		AutoGroups                    []string
		UsableGroups                  map[string]string
		GroupRatios                   map[string]float64
		FXFactor                      float64
	}{source, context.Metadata.UserGroup, context.UsingGroup, context.Metadata.AutoGroups, context.UsableGroups, context.GroupRatios, context.FxFactor})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}
