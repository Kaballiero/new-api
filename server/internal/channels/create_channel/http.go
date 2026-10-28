package create_channel

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
	var request AddChannelRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	if err := h.usecase(c).Execute(request); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccessStatus(c, http.StatusCreated, nil)
}
