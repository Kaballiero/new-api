package clear_disk_cache

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type Handler struct{ effect func() error }

func NewHandler(effect func() error) Handler { return Handler{effect: effect} }
func (h Handler) Handle(c *gin.Context) {
	err := h.effect()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "不活跃的磁盘缓存已清理",
	})
}
