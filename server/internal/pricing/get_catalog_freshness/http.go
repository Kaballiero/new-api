package get_catalog_freshness

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	common.ApiSuccess(c, h.usecase.Execute())
}
