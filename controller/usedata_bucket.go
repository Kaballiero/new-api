package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func parseQuotaGranularity(c *gin.Context) (string, bool) {
	granularity := c.Query("granularity")
	if granularity == "" || model.IsValidQuotaGranularity(granularity) {
		return granularity, true
	}
	common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_granularity", model.ErrInvalidQuotaGranularity.Error())
	return "", false
}

func respondAllBucketedQuotaDates(c *gin.Context, startTime int64, endTime int64, username string, granularity string) {
	if startTime <= 0 || endTime <= 0 {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "start_timestamp and end_timestamp must be positive")
		return
	}
	respondBucketedQuotaDates(c, model.BucketedQuotaQuery{StartTime: startTime, EndTime: endTime, Username: username, Granularity: granularity})
}

func respondBucketedQuotaDates(c *gin.Context, query model.BucketedQuotaQuery) {
	dates, err := model.GetBucketedQuotaDates(query)
	if err != nil {
		if errors.Is(err, model.ErrQuotaMonthSpanExceeded) {
			common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "month_span_exceeded", err.Error())
			return
		}
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccess(c, dates)
}
