package list_unbillable_models

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"slices"
	"strings"
)

const (
	unbillableReasonNoBillingConfig = "no_billing_config"
	unbillableReasonNoCatalogPrice  = "no_catalog_price"
	unbillableModelsGroup           = "default"
)

type Reader interface {
	Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error)
	FX() (domain.BillingFXBasis, error)
	AutoGroups(string) []string
	UsableGroups(string) map[string]string
	GroupRatio(string, string) float64
	Billable([]domain.Pricing, string, string) map[string]bool
	ModelReserves([]string, []string) (map[string]float64, error)
}
type Usecase struct {
	deps       domain.Capabilities
	reader     Reader
	projection *domain.Projection
}

func NewUsecase(deps domain.Capabilities, reader Reader, projection *domain.Projection) Usecase {
	return Usecase{deps: deps, reader: reader, projection: projection}
}
func (u Usecase) Execute() ([]domain.UnbillableModel, error) {
	context, err := u.consumerContext(unbillableModelsGroup, unbillableModelsGroup)
	if err != nil {
		return nil, err
	}
	names := []string{}
	catalogTypes := make(map[string]string)
	openRouterBilled := make(map[string]bool)
	for _, item := range context.Pricing {
		if len(domain.EffectivePricingGroups(item.EnableGroup, context.UsableGroups)) == 0 {
			continue
		}
		names = append(names, item.ModelName)
		catalogTypes[item.ModelName] = item.CatalogType
		for _, sources := range item.BillingSourcesForGroup(unbillableModelsGroup) {
			if slices.Contains(sources, domain.BillingSourceOpenRouterActualCost) {
				openRouterBilled[item.ModelName] = true
			}
		}
	}
	billable := u.reader.Billable(context.Pricing, unbillableModelsGroup, context.Metadata.CatalogRevision)
	reserves, err := u.reader.ModelReserves(names, []string{unbillableModelsGroup})
	if err != nil {
		return nil, err
	}
	suggested := CommonReservesByCatalogType(reserves, catalogTypes)

	result := []domain.UnbillableModel{}
	for _, name := range names {
		// billable is nil in self-use mode: every model is listed and billed by fallback ratios.
		if billable == nil || billable[name] {
			continue
		}
		entry := domain.UnbillableModel{ModelName: name, CatalogType: catalogTypes[name], Reason: unbillableReasonNoBillingConfig}
		// A reserve only applies to OpenRouter-billed models; batch aliases are reserved by their tiered expression.
		if reserve, ok := suggested[entry.CatalogType]; ok && openRouterBilled[name] && !strings.HasSuffix(name, ":batch") {
			entry.SuggestedReserveUSD = &reserve
		}
		result = append(result, entry)
	}
	for _, item := range domain.ConsumerPricingCatalogue(context.Pricing, context.UsableGroups) {
		if billable != nil && !billable[item.ModelName] {
			continue
		}
		// Reported when either public endpoint (models page or top models) would hide it.
		if !domain.HasConfirmedConsumerPrice(context.Project(item)) || !domain.PublicTariffConfirmed(u.projection.BuildPublicModelTariff(context, item)) {
			result = append(result, domain.UnbillableModel{ModelName: item.ModelName, CatalogType: item.CatalogType, Reason: unbillableReasonNoCatalogPrice})
		}
	}
	slices.SortFunc(result, func(left, right domain.UnbillableModel) int {
		if order := strings.Compare(left.Reason, right.Reason); order != 0 {
			return order
		}
		return domain.CompareEffectivePricingNames(left.ModelName, right.ModelName)
	})
	return result, nil
}

func CommonReservesByCatalogType(reserves map[string]float64, catalogTypes map[string]string) map[string]float64 {
	counts := make(map[string]map[float64]int)
	for name, reserve := range reserves {
		catalogType := catalogTypes[name]
		if catalogType == "" {
			continue
		}
		if counts[catalogType] == nil {
			counts[catalogType] = make(map[float64]int)
		}
		counts[catalogType][reserve]++
	}
	result := make(map[string]float64, len(counts))
	for catalogType, byReserve := range counts {
		best, bestCount := 0.0, 0
		for reserve, count := range byReserve {
			if count > bestCount || count == bestCount && reserve < best {
				best, bestCount = reserve, count
			}
		}
		result[catalogType] = best
	}
	return result
}

func (u Usecase) consumerContext(userGroup, usingGroup string) (domain.EffectivePricingPageContext, error) {
	ctx := domain.EffectivePricingPageContext{Policy: u.projection, Metadata: domain.ConsumerPricingResponse{SchemaVersion: "v2", UserGroup: userGroup, Currency: "RUB", AutoGroups: u.reader.AutoGroups(userGroup), Data: []domain.ConsumerPricingModel{}}, UsableGroups: u.reader.UsableGroups(userGroup), GroupRatios: make(map[string]float64), UsingGroup: usingGroup}
	if usingGroup != "" {
		if _, allowed := ctx.UsableGroups[usingGroup]; !allowed {
			return ctx, domain.ErrPricingUsingGroup
		}
	}
	basis, err := u.reader.FX()
	if err != nil {
		return ctx, err
	}
	ctx.FxFactor = basis.Factor
	source := ""
	ctx.Pricing, ctx.Vendors, source, err = u.reader.Snapshot()
	if err != nil {
		return ctx, fmt.Errorf("failed to resolve pricing catalog revision")
	}
	for group := range ctx.UsableGroups {
		ctx.GroupRatios[group] = u.reader.GroupRatio(userGroup, group)
	}
	if usingGroup != "" {
		ctx.UsableGroups = map[string]string{usingGroup: ctx.UsableGroups[usingGroup]}
	}
	ctx.Metadata.CatalogRevision, err = domain.ConsumerPricingRevision(u.deps, source, ctx)
	return ctx, err
}
