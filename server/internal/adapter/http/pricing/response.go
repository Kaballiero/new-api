package pricing

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
	"net/http"
)

func WriteFailure(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	body := gin.H{"success": false, "message": err.Error()}
	var failure *domain.ReadFailure
	if errors.As(err, &failure) {
		switch failure.Kind {
		case domain.InvalidQuery:
			status = http.StatusBadRequest
		case domain.UserUnavailable:
			status = http.StatusInternalServerError
		case domain.NotFound:
			status = http.StatusNotFound
		case domain.RevisionMismatch:
			status = http.StatusConflict
			body["code"] = "pricing_catalog_revision_mismatch"
		}
	}
	c.JSON(status, body)
}
