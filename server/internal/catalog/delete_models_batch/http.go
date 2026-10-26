package delete_models_batch

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	scenario Usecase
	audit    func(*gin.Context, Input, Output)
}

func NewHTTP(scenario Usecase, audit func(*gin.Context, Input, Output)) HTTP {
	return HTTP{scenario: scenario, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	input.AllowRemovePricing = c.GetInt("role") == common.RoleRootUser
	out, err := h.scenario.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit(c, input, out)
	common.ApiSuccess(c, out)
}
