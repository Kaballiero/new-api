package admin_list_topups

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	result, err := h.usecase.Execute(Input{Start: page.GetStartIdx(), Limit: page.GetPageSize(), Keyword: c.Query("keyword")})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	page.SetTotal(int(result.Total))
	page.SetItems(dto.FromOrders(result.Orders))
	common.ApiSuccess(c, page)
}
