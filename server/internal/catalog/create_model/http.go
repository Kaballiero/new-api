package create_model

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var value Request
	if err := c.ShouldBindJSON(&value); err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}
	out, err := h.scenario.Execute(Input{Value: domain.Model(value)})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccessStatus(c, http.StatusCreated, out)
}
