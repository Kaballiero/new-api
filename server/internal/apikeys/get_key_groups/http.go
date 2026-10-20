package get_key_groups

import (
	"github.com/QuantumNous/new-api/common"
	tokenhttp "github.com/QuantumNous/new-api/internal/adapter/apikeyhttp"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ factory func(*gin.Context) Reader }

func NewHTTP(factory func(*gin.Context) Reader) HTTP { return HTTP{factory: factory} }
func (h HTTP) Handle(c *gin.Context) {
	result, failure := NewUsecase(h.factory(c)).Execute()
	if failure != nil {
		tokenhttp.WriteFailure(c, failure)
		return
	}
	common.ApiSuccess(c, result)
}
