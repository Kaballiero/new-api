package update_channels_balances

import (
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	if err := h.usecase(c).Execute(); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, Output{Success: true, Message: ""})
}
