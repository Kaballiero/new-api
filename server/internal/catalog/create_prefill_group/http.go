package create_prefill_group

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var request transport.PrefillGroup
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, common.PreserveJSONTypeError(err, "github.com/QuantumNous/new-api/internal/catalog/transport", "model"))
		return
	}
	out, err := h.scenario.Execute(request.Value())
	if err != nil {
		transport.PrefillError(c, err)
		return
	}
	common.ApiSuccess(c, transport.PrefillResponse(out))
}
