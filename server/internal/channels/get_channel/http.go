package get_channel

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgChannelIdFormatError)
		return
	}
	channel, err := h.usecase(c).Execute(Input{ID: id})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, channel)
}
