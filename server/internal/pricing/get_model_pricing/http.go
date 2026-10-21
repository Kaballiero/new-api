package get_model_pricing

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(errors.New("invalid pricing query")))
		return
	}
	name, expected, err := transport.ParseEffectivePricingModelQuery(values)
	if err != nil {
		transport.WriteError(c, domain.InvalidQuery(err))
		return
	}
	input := Input{UserID: c.GetInt("id"), Name: name, ExpectedRevision: expected}
	output, err := h.usecase.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": output})
}
