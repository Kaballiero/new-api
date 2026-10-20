package list_accounts

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(reader Reader) *HTTP { return &HTTP{scenario: NewUsecase(reader)} }
func (h *HTTP) Handle(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	sortOptions := SortInput{By: c.Query("sort_by"), Order: c.Query("sort_order")}
	users, total, err := h.scenario.Execute(PageInput{Page: pageInfo.GetPage(), PageSize: pageInfo.GetPageSize()}, sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(dto.UsersFromDomain(users))

	common.ApiSuccess(c, pageInfo)
	return
}
