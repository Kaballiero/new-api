package delete_vendor

import (
	"net/http"
	"strconv"

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
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "invalid id")
		return
	}
	input := Input{ID: id}
	out, err := h.scenario.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit(c, input, *out)
	common.ApiSuccess(c, nil)
}
