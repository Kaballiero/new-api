package update_channel_status

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || (input.Status != common.ChannelStatusEnabled && input.Status != common.ChannelStatusManuallyDisabled) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	input.ID = id
	changed := h.usecase(c).Execute(input)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": changed})
}
