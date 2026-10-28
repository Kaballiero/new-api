package get_performance_stats

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Reader interface{ Stats() PerformanceStats }
type Handler struct{ reader Reader }

func NewHandler(reader Reader) Handler { return Handler{reader: reader} }
func (h Handler) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.reader.Stats()})
}
