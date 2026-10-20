package test_channel

import (
	"context"
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
	stream, _ := strconv.ParseBool(c.Query("stream"))
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	output, failure := h.scenario.Execute(Input{ID: id, Context: ctx, Model: c.Query("model"), Endpoint: c.Query("endpoint_type"), Stream: stream})
	if failure != nil {
		switch failure.Kind {
		case "missing":
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "channel_not_found", i18n.MsgChannelNotExists)
		case "actor":
			common.ApiErrorStatusCode(c, http.StatusBadRequest, "invalid_params", failure.Cause)
		default:
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", failure.Cause)
		}
		return
	}
	if output.Kind != "" {
		status := http.StatusBadGateway
		if output.Kind == "local" {
			status = http.StatusUnprocessableEntity
		}
		c.AbortWithStatusJSON(status, gin.H{"success": false, "code": "channel_test_failed", "message": output.Message, "time": output.Response.Time, "data": output.Response})
		return
	}
	common.ApiSuccess(c, output.Response)
}
