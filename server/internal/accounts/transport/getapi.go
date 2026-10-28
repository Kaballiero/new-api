package transport

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/gin-gonic/gin"
)

type GetAPIErrors struct{ IsMissing func(error) bool }

func (writer GetAPIErrors) Write(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	code := domain.ErrGetAPICredentialUnavailable.Error()
	switch {
	case errors.Is(err, domain.ErrGetAPIInvalidRequest), errors.Is(err, domain.ErrQuotaLimitExceeded):
		status = http.StatusBadRequest
		code = err.Error()
	case errors.Is(err, domain.ErrGetAPICapabilityDenied), errors.Is(err, domain.ErrGetAPITargetRoleDenied):
		status = http.StatusForbidden
		code = err.Error()
	case errors.Is(err, domain.ErrGetAPIAccountNotFound), writer.IsMissing(err):
		status = http.StatusNotFound
		code = domain.ErrGetAPIAccountNotFound.Error()
	case errors.Is(err, domain.ErrGetAPIQuotaCreditConflict), errors.Is(err, domain.ErrGetAPICreateConflict), errors.Is(err, domain.ErrGetAPIUsernameMismatch):
		status = http.StatusConflict
		code = err.Error()
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": "GetAPI request could not be completed"})
}
