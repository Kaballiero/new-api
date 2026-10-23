package admin_list_events

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/audit/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	query, err := transport.ReadQuery(c, page.Page, page.PageSize, page.GetStartIdx())
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	result, err := h.usecase.Execute(Input{Query: query, Viewer: transport.Actor(c)})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	page.SetItems(result.Items)
	page.SetTotal(int(result.Total))
	common.ApiSuccess(c, page)
}
