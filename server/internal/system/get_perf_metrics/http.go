package get_perf_metrics

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/transport"

	"github.com/gin-gonic/gin"
)

type Reader interface {
	Query(domain.QueryParams) (domain.QueryResult, error)
}
type Handler struct {
	reader Reader
	groups func() []string
}

func NewHandler(reader Reader, groups func() []string) Handler {
	return Handler{reader: reader, groups: groups}
}
func (h Handler) Handle(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": transport.Message(c, i18n.MsgSystemModelRequired),
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := h.reader.Query(domain.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	active := h.groups()
	filtered := make([]domain.GroupResult, 0, len(result.Groups))
	for _, group := range result.Groups {
		if slices.Contains(active, group.Group) {
			filtered = append(filtered, group)
		}
	}
	result.Groups = filtered

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
