package list_unbillable_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	models, err := h.usecase.Execute()
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccess(c, models)
}
