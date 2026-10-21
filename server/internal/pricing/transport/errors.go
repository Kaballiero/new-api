package transport

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
)

// WriteError writes err as the HTTP error response of a pricing read or
// upstream ratio sync. Errors without a domain meaning are written as
// {success:false} with HTTP 503 and the text of the error.
func WriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrRevisionMismatch):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error(), "code": "pricing_catalog_revision_mismatch"})
	case errors.Is(err, domain.ErrInvalidQuery), errors.Is(err, domain.ErrInvalidUserGroup):
		writeFailure(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrUserGroupUnavailable):
		writeFailure(c, http.StatusInternalServerError, err.Error())
	case errors.Is(err, domain.ErrChannelQueryFailed):
		writeFailure(c, http.StatusInternalServerError, common.TranslateMessage(c, i18n.MsgChannelQueryFailed))
	case errors.Is(err, domain.ErrModelPricingUnavailable):
		writeFailure(c, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrNoUpstreams):
		writeFailure(c, http.StatusOK, common.TranslateMessage(c, i18n.MsgChannelNoValidUpstream))
	default:
		writeFailure(c, http.StatusServiceUnavailable, err.Error())
	}
}

// UpstreamErrorText is the text of a failed upstream in the ratio sync test
// results.
func UpstreamErrorText(c *gin.Context, err error) string {
	if errors.Is(err, domain.ErrUpstreamDataUnrecognized) {
		return common.TranslateMessage(c, i18n.MsgPricingUpstreamDataUnrecognized)
	}
	return err.Error()
}

func writeFailure(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
}
