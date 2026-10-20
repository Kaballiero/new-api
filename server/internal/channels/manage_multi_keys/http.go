package manage_multi_keys

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var request MultiKeyManageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	output, failure := h.scenario.Execute(request)
	if failure != nil {
		if failure.Kind == "permission" {
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		} else {
			common.ApiError(c, failure.Cause)
		}
		return
	}
	response := gin.H{"success": output.Success, "message": output.Message}
	if output.Data != nil {
		response["data"] = output.Data
	}
	c.JSON(http.StatusOK, response)
}
