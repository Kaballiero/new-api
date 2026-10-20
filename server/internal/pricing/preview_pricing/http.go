package preview_pricing

import (
	"github.com/QuantumNous/new-api/common"
	pricinghttp "github.com/QuantumNous/new-api/internal/adapter/http/pricing"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	groups, supplied := c.Request.URL.Query()["user_group"]
	out, err := h.usecase.Execute(Input{UserID: c.GetInt("id"), UserGroups: groups, Supplied: supplied})
	if err != nil {
		pricinghttp.WriteFailure(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
