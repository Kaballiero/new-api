package delete_models_batch

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store                           Store
	refreshChannels, refreshPricing func()
	audit                           func(*gin.Context, Input, Output)
}

func NewHTTP(store Store, refreshChannels, refreshPricing func(), audit func(*gin.Context, Input, Output)) HTTP {
	return HTTP{store: store, refreshChannels: refreshChannels, refreshPricing: refreshPricing, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	input.AllowRemovePricing = c.GetInt("role") == common.RoleRootUser
	audit := func(input Input, result Output) { h.audit(c, input, result) }
	out, err := NewUsecase(h.store, h.refreshChannels, h.refreshPricing, audit).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
