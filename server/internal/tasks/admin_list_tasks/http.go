package admin_list_tasks

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(usecase Usecase) Handler { return Handler{usecase: usecase} }
func (h Handler) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	query := transport.ReadTaskQuery(c)

	result, _ := h.usecase.Execute(Input{UserID: c.GetInt("id"), Offset: page.GetStartIdx(), Limit: page.GetPageSize(), Query: query,
		Visibility: domain.Visibility{Admin: c.GetInt("role") >= common.RoleAdminUser, Root: c.GetInt("role") >= common.RoleRootUser}})
	page.SetTotal(int(result.Total))
	page.SetItems(result.Items)
	common.ApiSuccess(c, page)
}
