package transport

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"

	"github.com/gin-gonic/gin"
)

func ReadQuotaQuery(c *gin.Context) domain.QuotaQuery {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	return domain.QuotaQuery{StartTime: start, EndTime: end, Username: c.Query("username"), Granularity: c.Query("granularity")}
}

func ReadFlowRange(c *gin.Context) (domain.FlowQuery, bool) {
	start, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || start <= 0 {
		common.ApiErrorMsg(c, Message(c, domain.ErrUsageInvalidStart))
		return domain.FlowQuery{}, false
	}
	end, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || end <= 0 {
		common.ApiErrorMsg(c, Message(c, domain.ErrUsageInvalidEnd))
		return domain.FlowQuery{}, false
	}
	if end < start {
		common.ApiErrorMsg(c, Message(c, domain.ErrUsageInvalidRange))
		return domain.FlowQuery{}, false
	}
	return domain.FlowQuery{StartTime: start, EndTime: end}, true
}

func WriteQuotaError(c *gin.Context, err error) {
	code, status := "internal_error", 500
	switch {
	case errors.Is(err, domain.ErrQuotaSpan):
		code, status = "time_span_exceeded", 400
	case errors.Is(err, domain.ErrInvalidQuotaGranularity):
		code, status = "invalid_granularity", 400
	case errors.Is(err, domain.ErrPositiveQuotaTimes):
		code, status = "invalid_params", 400
	case errors.Is(err, domain.ErrQuotaMonthSpanExceeded):
		code, status = "month_span_exceeded", 400
	}
	common.ApiErrorMsgStatusCode(c, status, code, Message(c, err))
}
