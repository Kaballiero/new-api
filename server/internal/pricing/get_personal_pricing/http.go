package get_personal_pricing

import (
	pricinghttp "github.com/QuantumNous/new-api/internal/adapter/http/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	values, page, err := pricinghttp.ParseCatalogueMode(c.Request.URL.RawQuery)
	if err != nil {
		pricinghttp.WriteFailure(c, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: err.Error()})
		return
	}
	input := Input{UserID: c.GetInt("id"), Page: page}
	if input.Page {
		input.Query, err = pricinghttp.ParseEffectivePricingPageQuery(values, true)
		if err != nil {
			pricinghttp.WriteFailure(c, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: err.Error()})
			return
		}
	}
	output, err := h.usecase.Execute(input)
	if err != nil {
		pricinghttp.WriteFailure(c, err)
		return
	}
	if output.Page != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": output.Page})
		return
	}
	expected, err := pricinghttp.ExpectedPricingRevision(c.Request.URL.Query())
	if err != nil {
		pricinghttp.WriteFailure(c, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: err.Error()})
		return
	}
	if err = domain.CheckRevision(expected, output.Full.CatalogRevision); err != nil {
		pricinghttp.WriteFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output.Full})
}
