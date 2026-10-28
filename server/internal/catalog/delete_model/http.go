package delete_model

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
	removeFromChannels, err := strconv.ParseBool(c.DefaultQuery("remove_from_channels", "false"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	removePricing, err := strconv.ParseBool(c.DefaultQuery("remove_pricing", "false"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	input := Input{ModelIDs: []int{id}, RemoveFromChannels: removeFromChannels, RemovePricing: removePricing}
	input.AllowRemovePricing = c.GetInt("role") == common.RoleRootUser
	out, err := h.scenario.Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit(c, input, out)
	common.ApiSuccess(c, out)
}
