package get_current_system_task

import (
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/transport"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/dto"
	"github.com/gin-gonic/gin"
)

type Store interface {
	GetActiveSystemTask(string) (*domain.SystemTask, error)
}
type Handler struct {
	store  Store
	decode func(string) any
}

func NewHandler(store Store, decode func(string) any) Handler {
	return Handler{store: store, decode: decode}
}
func (h Handler) Handle(c *gin.Context) {
	taskType := c.Query("type")
	if taskType == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": transport.Message(c, i18n.MsgSystemTypeRequired),
		})
		return
	}

	task, err := h.store.GetActiveSystemTask(taskType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dto.TaskResponse(task, h.decode),
	})
}
