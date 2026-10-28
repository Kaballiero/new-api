package reset_model_ratios

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	if err := h.usecase.Execute(); err != nil {
		c.JSON(200, Output{Message: err.Error()})
		return
	}
	c.JSON(200, Output{Success: true, Message: common.TranslateMessage(c, i18n.MsgModelResetSuccess)})
}
