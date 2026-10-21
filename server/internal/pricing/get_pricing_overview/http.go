package get_pricing_overview

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(errors.New("invalid pricing query")))
		return
	}
	for key := range values {
		if !slices.Contains([]string{"limit", "search", "group", "using_group", "catalog_type", "catalog_type_missing"}, key) {
			transport.WriteError(c, domain.InvalidQuery(errors.New("invalid pricing query")))
			return
		}
	}
	query, err := transport.ParseEffectivePricingPageQuery(values, false)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(err))
		return
	}
	if !values.Has("limit") {
		query.Limit = 4
	}
	query.Overview = true
	input := Input{Group: strings.TrimSpace(values.Get("group")), UsingGroup: strings.TrimSpace(values.Get("using_group")), Query: query}
	output, err := h.usecase.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output})
}
