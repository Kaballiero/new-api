package catalog

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

func Failure(c *gin.Context, f *domain.Failure) {
	switch f.Kind {
	case "api":
		common.ApiErrorMsg(c, f.Error())
	case "plain_forbidden":
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": f.Error()})
	case "vendor", "vendor_conflict", "vendor_referenced":
		status := http.StatusBadRequest
		payload := gin.H{"success": false, "message": f.Error()}
		if f.Kind == "vendor_conflict" {
			status = http.StatusConflict
			payload["code"] = "VENDOR_CONFLICT"
		}
		if f.Kind == "vendor_referenced" {
			status = http.StatusConflict
			payload["code"] = "VENDOR_REFERENCED"
			payload["reference_counts"] = f.Counts
		}
		c.JSON(status, payload)
	case "sync_conflict":
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": f.Error()})
	case "sync":
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": f.Error()})
	default:
		status := http.StatusBadRequest
		switch f.Kind {
		case "internal_error":
			status = http.StatusInternalServerError
		case "model_not_found", "vendor_not_found":
			status = http.StatusNotFound
		case "model_name_exists", "vendor_name_exists":
			status = http.StatusConflict
		}
		common.ApiErrorMsgStatusCode(c, status, f.Kind, f.Error())
	}
}
