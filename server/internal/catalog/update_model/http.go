package update_model

import (
	"github.com/QuantumNous/new-api/common"
	response "github.com/QuantumNous/new-api/internal/adapter/http/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	statusOnly := c.Query("status_only") == "true"
	var value Request
	if err := c.ShouldBindJSON(&value); err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}
	out, failure := h.scenario.Execute(Input{Value: domain.Model(value), StatusOnly: statusOnly})
	if failure != nil {
		response.Failure(c, failure)
		return
	}
	common.ApiSuccess(c, out)
}
