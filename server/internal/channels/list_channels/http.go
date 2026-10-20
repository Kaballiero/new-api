package list_channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
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

	output, failure := h.scenario.Execute(Input{Filter: domain.Filter{Group: groupFilter, Status: statusFilter, Type: typeFilter}, Sort: sortOptions, TagMode: enableTagMode, Start: pageInfo.GetStartIdx(), Page: pageInfo.GetPage(), PageSize: pageInfo.GetPageSize()})
	if failure != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		return
	}
	common.ApiSuccess(c, output)
}
