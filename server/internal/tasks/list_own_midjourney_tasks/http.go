package list_own_midjourney_tasks

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(usecase Usecase) Handler { return Handler{usecase: usecase} }
func (h Handler) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	query := transport.ReadMidjourneyQuery(c)
	query.ChannelID = ""
	result, _ := h.usecase.Execute(Input{UserID: c.GetInt("id"), Offset: page.GetStartIdx(), Limit: page.GetPageSize(), Query: query})
	page.SetTotal(int(result.Total))
	page.SetItems(result.Items)
	common.ApiSuccess(c, page)
}
