package list_missing_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	out, err := h.scenario.Execute()
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
