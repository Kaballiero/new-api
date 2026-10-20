package delete_models_batch

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	response "github.com/QuantumNous/new-api/internal/adapter/http/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	input.AllowRemovePricing = c.GetInt("role") == common.RoleRootUser
	out, err := h.scenario.Execute(input)
	if err != nil {
		var f *domain.Failure
		if errors.As(err, &f) {
			response.Failure(c, f)
		} else {
			common.ApiError(c, err)
		}
		return
	}
	common.ApiSuccess(c, out)
}
