package search_channels

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
	keyword := c.Query("keyword")
	group := c.Query("group")
	modelKeyword := c.Query("model")
	statusParam := c.Query("status")
	statusFilter := domain.ParseStatus(statusParam)
	idSort, _ := strconv.ParseBool(c.Query("id_sort"))
	sortOptions := domain.Sort{By: c.Query("sort_by"), Order: c.Query("sort_order"), ID: idSort}
	enableTagMode, _ := strconv.ParseBool(c.Query("tag_mode"))
	typeParam := c.Query("type")
	typeFilter := -1
	if typeParam != "" {
		if tp, err := strconv.Atoi(typeParam); err == nil {
			typeFilter = tp
		}
	}

	page, _ := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	output, err := h.scenario.Execute(Input{Keyword: keyword, Group: group, Model: modelKeyword, Status: statusFilter, Type: typeFilter, Sort: sortOptions, TagMode: enableTagMode, Page: page, PageSize: pageSize})
	if err != nil {
		common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		return
	}
	common.ApiSuccess(c, output)
}
