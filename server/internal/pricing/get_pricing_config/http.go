package get_pricing_config

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	out, err := h.usecase.Execute(Input{Models: c.QueryArray("model")})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
