package fetch_upstream_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgChannelIdFormatError)
		return
	}
	output, failure := h.scenario.Execute(Input{ID: id})
	if failure != nil {
		switch failure.Kind {
		case "missing":
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
		case "internal":
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		default:
			common.ApiErrorMsgStatusCode(c, http.StatusBadGateway, "fetch_models_failed", failure.Message)
		}
		return
	}
	common.ApiSuccess(c, output)
}
