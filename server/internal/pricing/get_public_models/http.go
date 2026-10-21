package get_public_models

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	input, err := transport.ParsePublicPricingModelsQuery(c.Request.URL.RawQuery)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(err))
		return
	}
	output, err := h.usecase.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output})
}
