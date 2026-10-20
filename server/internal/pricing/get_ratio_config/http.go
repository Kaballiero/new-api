package get_ratio_config

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	out, enabled := h.usecase.Execute()
	if !enabled {
		common.ApiErrorMsgStatusCode(c, http.StatusForbidden, "ratio_config_disabled", "倍率配置接口未启用")
		return
	}
	common.ApiSuccess(c, out)
}
