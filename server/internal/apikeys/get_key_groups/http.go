package get_key_groups

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ factory func(*gin.Context) Reader }

func NewHTTP(factory func(*gin.Context) Reader) HTTP { return HTTP{factory: factory} }
func (h HTTP) Handle(c *gin.Context) {
	result, err := NewUsecase(h.factory(c)).Execute()
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
