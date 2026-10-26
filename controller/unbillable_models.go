package controller

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	// unbillableReasonNoBillingConfig: no price, ratio, tiered expression or OpenRouter reserve,
	// so /v1/models does not list the model and requests cannot be billed.
	unbillableReasonNoBillingConfig = "no_billing_config"
	// unbillableReasonNoCatalogPrice: billable, but no confirmed price for the public catalog.
	unbillableReasonNoCatalogPrice = "no_catalog_price"
	unbillableModelsGroup          = "default"
)

type unbillableModel struct {
	ModelName   string `json:"model_name"`
	CatalogType string `json:"catalog_type,omitempty"`
	Reason      string `json:"reason"`
	// SuggestedReserveUSD is the most common OpenRouter reserve among billable models of the same catalog type.
	SuggestedReserveUSD *float64 `json:"suggested_reserve_usd,omitempty"`
}

// GetUnbillableModels lists models enabled for the default group that public
// pricing hides because they cannot be billed or have no catalog price.
func GetUnbillableModels(c *gin.Context) {
	models, err := listUnbillableModels()
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccess(c, models)
}

func listUnbillableModels() ([]unbillableModel, error) {
	context, err := loadConsumerPricingContext(unbillableModelsGroup, unbillableModelsGroup)
	if err != nil {
		return nil, err
	}
	names := []string{}
	catalogTypes := make(map[string]string)
	openRouterBilled := make(map[string]bool)
	for _, item := range context.pricing {
		if len(effectivePricingGroups(item.EnableGroup, context.usableGroups)) == 0 {
			continue
		}
		names = append(names, item.ModelName)
		catalogTypes[item.ModelName] = item.CatalogType
		for _, sources := range item.BillingSourcesForGroup(unbillableModelsGroup) {
			if slices.Contains(sources, model.BillingSourceOpenRouterActualCost) {
				openRouterBilled[item.ModelName] = true
			}
		}
	}
	billable := billablePricingModels(context.pricing, unbillableModelsGroup, context.metadata.CatalogRevision)
	reserves, err := model.GetOpenRouterModelReserves(names, []string{unbillableModelsGroup})
	if err != nil {
		return nil, err
	}
	suggested := commonReservesByCatalogType(reserves, catalogTypes)

	result := []unbillableModel{}
	for _, name := range names {
		// billable is nil in self-use mode: every model is listed and billed by fallback ratios.
		if billable == nil || billable[name] {
			continue
		}
		entry := unbillableModel{ModelName: name, CatalogType: catalogTypes[name], Reason: unbillableReasonNoBillingConfig}
		// A reserve only applies to OpenRouter-billed models; batch aliases are reserved by their tiered expression.
		if reserve, ok := suggested[entry.CatalogType]; ok && openRouterBilled[name] && !strings.HasSuffix(name, ":batch") {
			entry.SuggestedReserveUSD = &reserve
		}
		result = append(result, entry)
	}
	for _, item := range model.ConsumerPricingCatalogue(context.pricing, context.usableGroups) {
		if billable != nil && !billable[item.ModelName] {
			continue
		}
		// Reported when either public endpoint (models page or top models) would hide it.
		if !hasConfirmedConsumerPrice(context.project(item)) || !publicTariffConfirmed(buildPublicModelTariff(context, item)) {
			result = append(result, unbillableModel{ModelName: item.ModelName, CatalogType: item.CatalogType, Reason: unbillableReasonNoCatalogPrice})
		}
	}
	slices.SortFunc(result, func(left, right unbillableModel) int {
		if order := strings.Compare(left.Reason, right.Reason); order != 0 {
			return order
		}
		return compareEffectivePricingNames(left.ModelName, right.ModelName)
	})
	return result, nil
}

func commonReservesByCatalogType(reserves map[string]float64, catalogTypes map[string]string) map[string]float64 {
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

const unbillableSummaryLimit = 20

// logUnbillableModels warns once per catalog refresh so a newly added model
// without a tariff is noticed before customers find it missing.
func logUnbillableModels(summary *pricingCatalogRefreshSummary) {
	models, err := listUnbillableModels()
	if err != nil {
		summary.HiddenPublicModelsError = err.Error()
		common.SysError("pricing: failed to list models hidden from public pricing: " + err.Error())
		return
	}
	summary.HiddenPublicModelsCount = len(models)
	if len(models) == 0 {
		return
	}
	entries := make([]string, 0, len(models))
	for _, item := range models {
		entries = append(entries, item.ModelName+" ("+item.Reason+")")
	}
	summary.HiddenPublicModels = entries[:min(len(entries), unbillableSummaryLimit)]
	common.SysError(fmt.Sprintf("pricing: %d enabled models are hidden from public pricing and need configuration: %s", len(entries), strings.Join(entries, ", ")))
}
