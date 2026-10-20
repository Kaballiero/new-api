package get_model_pricing

import (
	pricinghttp "github.com/QuantumNous/new-api/internal/adapter/http/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		pricinghttp.WriteFailure(c, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: "invalid pricing query"})
		return
	}
	name, expected, err := pricinghttp.ParseEffectivePricingModelQuery(values)
	if err != nil {
		pricinghttp.WriteFailure(c, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: err.Error()})
		return
	}
	input := Input{UserID: c.GetInt("id"), Name: name, ExpectedRevision: expected}
	output, err := h.usecase.Execute(input)
	if err != nil {
		pricinghttp.WriteFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output})
}
