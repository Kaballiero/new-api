package admin_list_logs

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	query := transport.ReadLogQuery(c)
	result, err := h.usecase.Execute(Input{Query: query, Audience: transport.Audience(c)})
	if err != nil {
		transport.WriteLogError(c, err)
		return
	}
	page.SetTotal(int(result.Total))
	page.SetItems(result.Items)
	common.ApiSuccess(c, page)
}
