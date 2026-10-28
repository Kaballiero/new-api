package get_group_pricing

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase    Usecase
	modelTypes func() []string
}

func NewHTTP(usecase Usecase, modelTypes func() []string) HTTP {
	return HTTP{usecase: usecase, modelTypes: modelTypes}
}
func (h HTTP) Handle(c *gin.Context) {
	values, page, err := transport.ParseCatalogueMode(c.Request.URL.RawQuery)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(err))
		return
	}
	input := Input{UserID: c.GetInt("id"), Page: page}
	if input.Page {
		input.Query, err = transport.ParseEffectivePricingPageQuery(h.modelTypes(), values, false)
		if err != nil {
			transport.WriteError(c, domain.InvalidQuery(err))
			return
		}
	}
	if !input.Page && len(c.QueryArray("group")) > 1 {
		transport.WriteError(c, domain.ErrInvalidUserGroup)
		return
	}
	input.Group = strings.TrimSpace(values.Get("group"))
	input.UsingGroup = strings.TrimSpace(values.Get("using_group"))
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
