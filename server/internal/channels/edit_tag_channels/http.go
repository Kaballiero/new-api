package edit_tag_channels

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
	var input ChannelTag
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorI18n(c, i18n.MsgChannelParamsError)
		return
	}
	if err := h.usecase(c).Execute(input); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
