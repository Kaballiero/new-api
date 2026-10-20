package controller

import (
	pricingquery "github.com/QuantumNous/new-api/internal/adapter/http/pricing"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	"net/url"

	"github.com/gin-gonic/gin"
)

func expectedPricingRevision(values url.Values) (string, error) {
	return pricingquery.ExpectedPricingRevision(values)
}

func pricingRevisionMatches(c *gin.Context, expected, actual string) bool {
	if err := pricingdomain.CheckRevision(expected, actual); err != nil {
		pricingquery.WriteFailure(c, err)
		return false
	}
	return true
}

func consumerPricingRevision(source string, context effectivePricingPageContext) (string, error) {
	return pricingdomain.ConsumerPricingRevision(source, pricingdomain.EffectivePricingPageContext(context))
}
