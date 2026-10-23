package transport

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
)

const maxAggregateRequestBytes = 16 << 10

const maxAggregateResponseBytes = 4 << 20

func ReadAggregateRequest(c *gin.Context, request any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAggregateRequestBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		common.ApiErrorMsgStatusCode(c, http.StatusRequestEntityTooLarge, "aggregate_request_too_large", Message(c, domain.ErrUsageAggregateRequestSize))
		return false
	}
	if err != nil || common.Unmarshal(body, request) != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", Message(c, domain.ErrUsageAggregateJSON))
		return false
	}
	return true
}

func RespondAggregate(c *gin.Context, data any, err error) {
	if err == nil {
		var body []byte
		body, err = common.Marshal(gin.H{"success": true, "message": "", "data": data})
		if err == nil && len(body) <= maxAggregateResponseBytes {
			c.Data(http.StatusOK, "application/json; charset=utf-8", body)
			return
		}
		if err == nil {
			err = domain.ErrAggregateTooLarge
		}
	}
	switch {
	case errors.Is(err, domain.ErrInvalidAggregate):
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", Message(c, err))
	case errors.Is(err, domain.ErrAggregateUserNotFound):
		common.ApiErrorMsgStatusCode(c, http.StatusNotFound, "aggregate_user_not_found", Message(c, err))
	case errors.Is(err, domain.ErrAggregateTooLarge):
		common.ApiErrorMsgStatusCode(c, http.StatusUnprocessableEntity, "aggregate_response_too_large", Message(c, err))
	case errors.Is(err, domain.ErrAggregateOverflow):
		common.ApiErrorMsgStatusCode(c, 500, "aggregate_overflow", Message(c, err))
	default:
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
	}
}
