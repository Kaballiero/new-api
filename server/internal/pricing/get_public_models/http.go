package get_public_models

import (
	pricinghttp "github.com/QuantumNous/new-api/internal/adapter/http/pricing"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	input, err := pricinghttp.ParsePublicPricingModelsQuery(c.Request.URL.RawQuery)
	if err != nil {
		pricinghttp.WriteFailure(c, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: err.Error()})
		return
	}
	output, err := h.usecase.Execute(input)
	if err != nil {
		pricinghttp.WriteFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output})
}
