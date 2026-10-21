package validate_models

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	var input ValidateModelsRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	output, err := h.usecase(c).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, output)
}
