package preview_metadata_sync

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	out, err := h.scenario.Execute(c.Request.Context(), Input{Locale: c.Query("locale")})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
