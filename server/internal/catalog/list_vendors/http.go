package list_vendors

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	out, err := h.scenario.Execute(Input{Keyword: c.Query("keyword"), Association: c.Query("association"), Page: page.GetPage(), PageSize: page.GetPageSize()})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
