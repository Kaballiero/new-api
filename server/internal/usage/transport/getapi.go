package transport

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/gin-gonic/gin"
)

func ReadGetAPIUsageRequest(c *gin.Context, request any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxAggregateRequestBytes))
	if err != nil || common.Unmarshal(body, request) != nil {
		RespondGetAPIUsage(c, nil, domain.ErrGetAPIUsageInvalidQuery)
		return false
	}
	return true
}

func RespondGetAPIUsage(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
		return
	}
	status, code, key := http.StatusServiceUnavailable, "GETAPI_USAGE_UNAVAILABLE", i18n.MsgUsageQueryFailed
	switch {
	case errors.Is(err, domain.ErrGetAPIUsageInvalidQuery):
		status, code, key = http.StatusBadRequest, "GETAPI_USAGE_INVALID_QUERY", i18n.MsgInvalidParams
	case errors.Is(err, domain.ErrGetAPIUsageUnsupported):
		status, code, key = http.StatusNotImplemented, "GETAPI_USAGE_UNSUPPORTED", i18n.MsgUsageGetAPIUnsupported
	case errors.Is(err, domain.ErrAggregateTooLarge):
		status, code, key = http.StatusUnprocessableEntity, "GETAPI_USAGE_TOO_LARGE", i18n.MsgUsageAggregateResponseSize
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": i18n.T(c, key)})
}
