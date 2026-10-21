package get_ratio_config

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	out, enabled := h.usecase.Execute()
	if !enabled {
		common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "ratio_config_disabled", i18n.MsgPricingRatioConfigDisabled)
		return
	}
	common.ApiSuccess(c, out)
}
