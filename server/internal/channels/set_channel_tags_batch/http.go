package set_channel_tags_batch

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgChannelParamsError)
		return
	}
	count, err := h.usecase(c).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": count})
}
