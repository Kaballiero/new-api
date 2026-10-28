package preview_vendor_operation

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		transport.WriteError(c, domain.RejectVendorChange(err, false))
		return
	}
	out, err := h.scenario.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
