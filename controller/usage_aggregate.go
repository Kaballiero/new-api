package controller

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const maxAggregateRequestBytes = 16 << 10
const maxAggregateResponseBytes = 4 << 20

func GetLogAggregate(c *gin.Context) {
	var request model.AggregateScope
	if !readAggregateRequest(c, &request) {
		return
	}
	result, err := model.GetLogAggregate(request)
	respondAggregate(c, result, err)
}

func GetUsageAggregate(c *gin.Context) {
	var request model.UsageAggregateQuery
	if !readAggregateRequest(c, &request) {
		return
	}
	result, err := model.GetUsageAggregate(request)
	respondAggregate(c, result, err)
}

func readAggregateRequest(c *gin.Context, request any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAggregateRequestBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		common.ApiErrorMsgStatusCode(c, http.StatusRequestEntityTooLarge, "aggregate_request_too_large", "aggregate request exceeds 16 KiB")
		return false
	}
	if err != nil || common.Unmarshal(body, request) != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "invalid aggregate JSON body")
		return false
	}
	return true
}

func respondAggregate(c *gin.Context, data any, err error) {
	if err == nil {
		var body []byte
		body, err = common.Marshal(gin.H{"success": true, "message": "", "data": data})
		if err == nil && len(body) <= maxAggregateResponseBytes {
			c.Data(http.StatusOK, "application/json; charset=utf-8", body)
			return
		}
		if err == nil {
			err = model.ErrAggregateTooLarge
		}
	}
	switch {
	case errors.Is(err, model.ErrInvalidAggregate):
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
	case errors.Is(err, model.ErrAggregateUserNotFound):
		common.ApiErrorMsgStatusCode(c, http.StatusNotFound, "aggregate_user_not_found", err.Error())
	case errors.Is(err, model.ErrAggregateTooLarge):
		common.ApiErrorMsgStatusCode(c, http.StatusUnprocessableEntity, "aggregate_response_too_large", err.Error())
	case errors.Is(err, model.ErrAggregateOverflow):
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "aggregate_overflow", err)
	default:
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
	}
}
