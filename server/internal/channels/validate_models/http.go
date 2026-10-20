package validate_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func WriteFailure(c *gin.Context, failure *domain.Failure) {
	switch failure.Kind {
	case "missing":
		common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
	case "internal":
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
	case "actor":
		common.ApiErrorStatusCode(c, http.StatusBadRequest, "invalid_params", failure.Cause)
	default:
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", failure.Message)
	}
}
func (h HTTP) Handle(c *gin.Context) {
	var input ValidateModelsRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	output, failure := h.scenario.Execute(input)
	if failure != nil {
		WriteFailure(c, failure)
		return
	}
	common.ApiSuccess(c, output)
}
