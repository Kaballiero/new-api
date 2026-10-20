package delete_channels_batch

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store   Store
	refresh func()
	reset   func()
	audit   func(int64)
}

func NewHTTP(store Store, refresh func(), reset func(), audit func(int64)) HTTP {
	return HTTP{store: store, refresh: refresh, reset: reset, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store, h.refresh, h.reset, h.audit)
	_ = scenario
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
		return
	}
	rows, err := scenario.Execute(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": rows})
}
