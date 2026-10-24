package admin_list_user_subscriptions

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/dto"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	rows, err := h.usecase.Execute(id)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.Summaries(rows))
}
