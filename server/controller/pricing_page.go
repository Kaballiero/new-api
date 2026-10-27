package controller

import (
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	pricingquery "github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/QuantumNous/new-api/pkg/catalogsync"

	"net/url"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type effectivePricingCreator = pricingdomain.EffectivePricingCreator

type effectivePricingPageItem = pricingdomain.EffectivePricingPageItem

type effectivePricingPageData = pricingdomain.EffectivePricingPageData

type effectivePricingPageQuery = pricingdomain.EffectivePricingPageQuery

type effectivePricingPageContext pricingdomain.EffectivePricingPageContext

func parseEffectivePricingPageQuery(values url.Values, personal bool) (effectivePricingPageQuery, error) {
	return pricingquery.ParseEffectivePricingPageQuery(catalogsync.ModelTypes(), values, personal)
}

func buildEffectivePricingPage(context effectivePricingPageContext, query effectivePricingPageQuery, project func(model.Pricing) consumerPricingModel) effectivePricingPageData {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildEffectivePricingPage(pricingdomain.EffectivePricingPageContext(context), query, project)
}

func compareEffectivePricingNames(left, right string) int {
	return pricingdomain.CompareEffectivePricingNames(left, right)
}

func effectivePricingModality(item model.Pricing) string {
	return pricingdomain.EffectivePricingModality(item)
}

func effectivePricingPageKeys(item consumerPricingModel, orderBy string) (string, *float64) {
	return pricingdomain.EffectivePricingPageKeys(item, orderBy)
}

func trimPricingText(value string) string {
	return pricingdomain.TrimPricingText(value)
}

// GetEffectivePricingOverview captures one context for every bounded section.
func GetEffectivePricingOverview(c *gin.Context) { pricingHTTP().GetPricingOverview.Handle(c) }

type effectivePricingOverviewData = pricingdomain.EffectivePricingOverviewData

func buildEffectivePricingOverview(context effectivePricingPageContext, query effectivePricingPageQuery) effectivePricingOverviewData {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildEffectivePricingOverview(pricingdomain.EffectivePricingPageContext(context), query)
}

func (context effectivePricingPageContext) project(item model.Pricing) consumerPricingModel {
	context.Policy = app.NewPricingProjection()
	return pricingdomain.EffectivePricingPageContext(context).Project(item)
}
