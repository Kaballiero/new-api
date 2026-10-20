package delete_vendor

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	response "github.com/QuantumNous/new-api/internal/adapter/http/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "invalid id")
		return
	}
	input := Input{ID: id}
	_, err = h.scenario.Execute(input)
	if err != nil {
		kind := "vendor"
		if h.scenario.store.IsMissing(err) {
			response.Failure(c, &domain.Failure{Kind: "vendor_not_found", Message: "vendor not found"})
			return
		}
		if errors.Is(err, h.scenario.store.VendorConflict()) {
			kind = "vendor_conflict"
		}
		var references *domain.VendorReferenceError
		failure := &domain.Failure{Kind: kind, Cause: err}
		if errors.As(err, &references) {
			failure.Kind = "vendor_referenced"
			failure.Counts = references.Counts
		}
		response.Failure(c, failure)
		return
	}
	common.ApiSuccess(c, nil)
}
