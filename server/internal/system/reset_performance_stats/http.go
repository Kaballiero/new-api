package reset_performance_stats

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct{ effect func() }

func NewHandler(effect func()) Handler { return Handler{effect: effect} }
func (h Handler) Handle(c *gin.Context) {
	h.effect()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "统计信息已重置",
	})
}
