// Package transport holds the incoming HTTP code shared by catalog scenarios.
package transport

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/gin-gonic/gin"
)

// WriteError writes err as the HTTP error response of a catalog scenario.
// Errors without a domain meaning are written as {success:false} with HTTP 200
// and the text of the error.
func WriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrModelNotFound):
		common.ApiErrorMsgStatusCode(c, http.StatusNotFound, "model_not_found", "model not found")
	case errors.Is(err, domain.ErrVendorNotFound):
		common.ApiErrorMsgStatusCode(c, http.StatusNotFound, "vendor_not_found", "vendor not found")
	case errors.Is(err, domain.ErrModelIDMissing):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "model_id_missing", i18n.MsgModelIdMissing)
	case errors.Is(err, domain.ErrModelNameEmpty):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "model_name_empty", i18n.MsgModelNameEmpty)
	case errors.Is(err, domain.ErrModelNameRequired):
		common.ApiErrorI18n(c, i18n.MsgModelNameEmpty)
	case errors.Is(err, domain.ErrModelNameExists):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "model_name_exists", i18n.MsgModelNameExists)
	case errors.Is(err, domain.ErrVendorIDMissing):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "vendor_id_missing", i18n.MsgVendorIdMissing)
	case errors.Is(err, domain.ErrVendorNameEmpty):
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "vendor_name_empty", i18n.MsgVendorNameEmpty)
	case errors.Is(err, domain.ErrVendorNameExists):
		common.ApiErrorI18nStatusCode(c, http.StatusConflict, "vendor_name_exists", i18n.MsgVendorNameExists)
	case errors.Is(err, domain.ErrInvalidSquareState):
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "Invalid model square state")
	case errors.Is(err, domain.ErrInvalidPagination):
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "Invalid pagination")
	case errors.Is(err, domain.ErrPricingRootOnly):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Model pricing is managed by a super administrator."})
	case errors.Is(err, domain.ErrSyncSourceChanged):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "Upstream metadata changed; preview again"})
	case errors.Is(err, domain.ErrSyncModelUnavailable):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "Selected upstream model is no longer available"})
	case errors.Is(err, domain.ErrSyncRejected):
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrMetadataSyncConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, domain.ErrVendorRejected):
		// References to the vendor and stale previews are conflicts, anything
		// else is a bad request.
		status, payload := http.StatusBadRequest, gin.H{"success": false, "message": err.Error()}
		var references *domain.VendorReferenceError
		if errors.As(err, &references) {
			status, payload["code"], payload["reference_counts"] = http.StatusConflict, "VENDOR_REFERENCED", references.Counts
		} else if errors.Is(err, domain.ErrVendorConflict) {
			status, payload["code"] = http.StatusConflict, "VENDOR_CONFLICT"
		}
		c.JSON(status, payload)
	case errors.Is(err, domain.ErrInternal):
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
	default:
		common.ApiError(c, err)
	}
}
