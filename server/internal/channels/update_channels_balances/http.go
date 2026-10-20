package update_channels_balances

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	if err := h.scenario.Execute(); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, Output{Success: true, Message: ""})
}
