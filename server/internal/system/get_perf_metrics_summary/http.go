package get_perf_metrics_summary

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/gin-gonic/gin"
)

type Reader interface {
	Summary(int, []string) (domain.SummaryAllResult, error)
}
type Handler struct {
	reader Reader
	groups func() []string
}

func NewHandler(reader Reader, groups func() []string) Handler {
	return Handler{reader: reader, groups: groups}
}
func (h Handler) Handle(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	activeGroups := h.groups()
	result, err := h.reader.Summary(hours, activeGroups)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
