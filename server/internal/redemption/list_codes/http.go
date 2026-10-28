package list_codes

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/redemption/dto"
	"github.com/QuantumNous/new-api/internal/redemption/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase Usecase
}

func NewHTTP(usecase Usecase) HTTP {
	return HTTP{usecase: usecase}
}

func (h HTTP) Handle(c *gin.Context) {

	page := common.GetPageQuery(c)
	result, err := h.usecase.Execute(Input{Start: page.GetStartIdx(), Limit: page.GetPageSize()})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	page.SetTotal(int(result.Total))
	page.SetItems(dto.FromCodes(result.Codes))
	common.ApiSuccess(c, page)
}
