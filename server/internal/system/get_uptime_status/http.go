package get_uptime_status

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/internal/system/domain"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
	groups  func() []map[string]any
}

func NewHandler(usecase Usecase, groups func() []map[string]any) Handler {
	return Handler{usecase: usecase, groups: groups}
}
func (h Handler) Handle(c *gin.Context) {
	groups := h.groups()
	if len(groups) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": []domain.UptimeGroupResult{}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	results, _ := h.usecase.Execute(ctx, groups)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": results})
}
