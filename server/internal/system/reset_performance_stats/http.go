package reset_performance_stats

import (
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ effect func() }

func NewHandler(effect func()) Handler { return Handler{effect: effect} }
func (h Handler) Handle(c *gin.Context) {
	h.effect()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": transport.Message(c, i18n.MsgSystemStatsReset),
	})
}
