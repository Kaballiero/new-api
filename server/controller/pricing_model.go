package controller

import (
	pricingquery "github.com/QuantumNous/new-api/internal/adapter/http/pricing"
	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"net/url"

	"github.com/gin-gonic/gin"
)

type consumerPricingModelDetail = pricingdomain.ConsumerPricingModelDetail

func parseEffectivePricingModelQuery(values url.Values) (string, string, error) {
	return pricingquery.
		ParseEffectivePricingModelQuery(values)
}

func GetEffectivePricingModel(c *gin.Context) { app.NewGetEffectivePricingModelHTTP().Handle(c) }

func buildEffectivePricingModelDetail(context effectivePricingPageContext, name string) (consumerPricingModelDetail, bool) {
	context.Policy = app.NewPricingProjection()
	return app.
		NewPricingProjection().BuildEffectivePricingModelDetail(pricingdomain.EffectivePricingPageContext(context), name)
}
