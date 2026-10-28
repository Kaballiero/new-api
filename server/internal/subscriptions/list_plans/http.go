package list_plans

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/dto"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	rows, err := h.usecase.Execute()
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.Plans(rows))
}
