package get_personal_pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	values, page, err := transport.ParseCatalogueMode(c.Request.URL.RawQuery)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(err))
		return
	}
	input := Input{UserID: c.GetInt("id"), Page: page}
	if input.Page {
		input.Query, err = transport.ParseEffectivePricingPageQuery(values, true)
		if err != nil {
			transport.WriteError(c, domain.InvalidQuery(err))
			return
		}
	}
	output, err := h.usecase.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if output.Page != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": output.Page})
		return
	}
	expected, err := transport.ExpectedPricingRevision(c.Request.URL.Query())
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(err))
		return
	}
	if err = domain.CheckRevision(expected, output.Full.CatalogRevision); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output.Full})
}
