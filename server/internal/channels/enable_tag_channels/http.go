package enable_tag_channels

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store   Store
	refresh func()
	audit   func(string)
}

func NewHTTP(store Store, refresh func(), audit func(string)) HTTP {
	return HTTP{store: store, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store, h.refresh, h.audit)
	_ = scenario
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || input.Tag == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if err := scenario.Execute(input); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
