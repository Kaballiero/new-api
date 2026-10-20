package set_channel_tags_batch

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store   Store
	refresh func()
	audit   func(int)
}

func NewHTTP(store Store, refresh func(), audit func(int)) HTTP {
	return HTTP{store: store, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store, h.refresh, h.audit)
	_ = scenario
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
		return
	}
	count, err := scenario.Execute(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": count})
}
