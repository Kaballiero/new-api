package list_channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
	"strconv"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := domain.Sort{By: c.Query("sort_by"), Order: c.Query("sort_order"), ID: idSort}
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	groupFilter := domain.NormalizeGroup(c.Query("group"))
	statusParam := c.Query("status")
	// statusFilter: -1 all, 1 enabled, 0 disabled (include auto & manual)
	statusFilter := domain.ParseStatus(statusParam)
	// type filter
	typeStr := c.Query("type")
	typeFilter := -1
	if typeStr != "" {
		if t, err := strconv.Atoi(typeStr); err == nil {
			typeFilter = t
		}
	}
	output, err := h.usecase(c).Execute(Input{Filter: domain.Filter{Group: groupFilter, Status: statusFilter, Type: typeFilter}, Sort: sortOptions, TagMode: enableTagMode, Start: pageInfo.GetStartIdx(), Page: pageInfo.GetPage(), PageSize: pageInfo.GetPageSize()})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, output)
}
