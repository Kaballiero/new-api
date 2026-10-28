package update_marketplace_sources

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	var sources []domain.MarketplaceSource
	if err := c.ShouldBindJSON(&sources); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	input := Input{Sources: sources}
	value, err := h.usecase.Execute(transport.Context(c), input)
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, value)
}
