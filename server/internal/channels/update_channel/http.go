package update_channel

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var request PatchChannel
	raw, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := common.Unmarshal(raw, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	var fields map[string]any
	if err := common.Unmarshal(raw, &fields); err != nil {
		common.ApiError(c, err)
		return
	}
	output, failure := h.scenario.Execute(Input{Request: request, Fields: fields, Raw: raw})
	if failure != nil {
		switch failure.Kind {
		case "invalid":
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		case "missing":
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
		case "validation":
			common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "channel_validation_failed", failure.Message)
		case "plugin":
			common.ApiErrorMsg(c, failure.Message)
		case "permission":
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		default:
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		}
		return
	}
	common.ApiSuccess(c, output)
}
