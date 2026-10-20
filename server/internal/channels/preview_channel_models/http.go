package preview_channel_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var input FetchModelsRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	output, failure := h.scenario.Execute(input)
	if failure != nil {
		if failure.Kind == "invalid" {
			common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", failure.Message)
		} else {
			common.ApiErrorMsgStatusCode(c, http.StatusBadGateway, "fetch_models_failed", failure.Message)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": output})
}
