package update_channel_status_batch

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Ids) == 0 || (input.Status != common.ChannelStatusEnabled && input.Status != common.ChannelStatusManuallyDisabled) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	changed := h.usecase(c).Execute(input)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": changed})
}
