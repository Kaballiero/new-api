package controller

import (
	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	pricingquery "github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/QuantumNous/new-api/pkg/catalogsync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"

	"github.com/gin-gonic/gin"
)

type publicPricingModelsPage = pricingdomain.PublicPricingModelsPage

type publicModelTariff = pricingdomain.PublicModelTariff

type publicPricingTier = pricingdomain.PublicPricingTier

type publicPricingCondition = pricingdomain.PublicPricingCondition

type publicPricingModelsQuery = pricingdomain.PublicPricingModelsQuery

func parsePublicPricingModelsQuery(raw string) (publicPricingModelsQuery, error) {
	return pricingquery.
		ParsePublicPricingModelsQuery(catalogsync.ModelTypes(), raw)
}

func GetPublicPricingModels(c *gin.Context) { pricingHTTP().GetPublicModels.Handle(c) }

func buildPublicPricingModelsPage(context effectivePricingPageContext, query publicPricingModelsQuery) publicPricingModelsPage {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildPublicPricingModelsPage(pricingdomain.EffectivePricingPageContext(context), query)
}

// The public catalog always projects the default tariff, independently of the user's usable groups.

func publicPricingGuard(guard billingexpr.TierCondition, units ...map[string]string) publicPricingCondition {
	return pricingdomain.PublicPricingGuard(pricingadapter.ToExpressionCondition(guard), units...)
}

func buildPublicModelTariff(context effectivePricingPageContext, item model.Pricing) publicModelTariff {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildPublicModelTariff(pricingdomain.EffectivePricingPageContext(context), item)
}

func buildCatalogModelTariff(context effectivePricingPageContext, item model.Pricing, group string) publicModelTariff {
	context.Policy = app.NewPricingProjection()
	return app.NewPricingProjection().BuildCatalogModelTariff(pricingdomain.EffectivePricingPageContext(context), item, group)
}

// Independent component minima do not prove that a complete request is free.
