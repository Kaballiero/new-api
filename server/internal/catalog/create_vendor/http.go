package create_vendor

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	scenario Usecase
	audit    func(*gin.Context, int, string)
}

func NewHTTP(scenario Usecase, audit func(*gin.Context, int, string)) HTTP {
	return HTTP{scenario: scenario, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var value Request
	if err := c.ShouldBindJSON(&value); err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}
	out, err := h.scenario.Execute(Input{Value: domain.Vendor(value)})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit(c, out.Id, out.Name)
	common.ApiSuccessStatus(c, http.StatusCreated, out)
}
