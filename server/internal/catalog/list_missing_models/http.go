package list_missing_models

import (
	"github.com/QuantumNous/new-api/common"
	response "github.com/QuantumNous/new-api/internal/adapter/http/catalog"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	out, f := h.scenario.Execute()
	if f != nil {
		response.Failure(c, f)
		return
	}
	common.ApiSuccess(c, out)
}
