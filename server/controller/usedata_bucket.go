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

func validQuotaBucketRange(c *gin.Context, startTime int64, endTime int64) bool {
	if startTime <= 0 || endTime <= 0 {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "start_timestamp and end_timestamp must be positive")
		return false
	}
	return true
}

func respondAllBucketedQuotaDates(c *gin.Context, startTime int64, endTime int64, username string, granularity string) {
	if !validQuotaBucketRange(c, startTime, endTime) {
		return
	}
	grouping := model.QuotaBucketByModel
	if username != "" {
		grouping = model.QuotaBucketByUserAndModel
	}
	respondBucketedQuotaDates(c, model.BucketedQuotaQuery{StartTime: startTime, EndTime: endTime, Username: username, Granularity: granularity, Grouping: grouping})
}

func respondBucketedQuotaDates(c *gin.Context, query model.BucketedQuotaQuery) {
	dates, err := model.GetBucketedQuotaDates(query)
	if err != nil {
		respondQuotaBucketError(c, err)
		return
	}
	common.ApiSuccess(c, dates)
}

func respondUserBucketedQuotaDates(c *gin.Context, startTime int64, endTime int64, granularity string) {
	if !validQuotaBucketRange(c, startTime, endTime) {
		return
	}
	dates, err := model.GetBucketedQuotaDatesByUser(model.BucketedQuotaQuery{StartTime: startTime, EndTime: endTime, Granularity: granularity, Grouping: model.QuotaBucketByUser})
	if err != nil {
		respondQuotaBucketError(c, err)
		return
	}
	common.ApiSuccess(c, dates)
}

func respondQuotaBucketError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrQuotaMonthSpanExceeded) {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "month_span_exceeded", err.Error())
		return
	}
	common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
}
