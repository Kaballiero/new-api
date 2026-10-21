package search_accounts

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(reader Reader) *HTTP { return &HTTP{scenario: NewUsecase(reader)} }
func (h *HTTP) Handle(c *gin.Context) {
	keyword := c.Query("keyword")
	group := c.Query("group")
	var role *int
	if roleStr := c.Query("role"); roleStr != "" {
		if parsed, err := strconv.Atoi(roleStr); err == nil {
			role = &parsed
		}
	}
	var status *int
	if statusStr := c.Query("status"); statusStr != "" {
		if parsed, err := strconv.Atoi(statusStr); err == nil {
			status = &parsed
		}
	}
	pageInfo := common.GetPageQuery(c)
	sortOptions := SortInput{By: c.Query("sort_by"), Order: c.Query("sort_order")}
	users, total, err := h.scenario.Execute(Input{Keyword: keyword, Group: group, Role: role, Status: status, Page: PageInput{Page: pageInfo.GetPage(), PageSize: pageInfo.GetPageSize()}, Sort: sortOptions})
	if err != nil {
		transport.WriteError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(dto.UsersFromDomain(users))
	common.ApiSuccess(c, pageInfo)
	return
}
