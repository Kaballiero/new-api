package get_system_task

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/dto"
	"github.com/QuantumNous/new-api/internal/system/transport"

	"github.com/gin-gonic/gin"
)

type Store interface {
	GetSystemTaskByTaskID(string) (*domain.SystemTask, error)
}
type Handler struct {
	store  Store
	decode func(string) any
}

func NewHandler(store Store, decode func(string) any) Handler {
	return Handler{store: store, decode: decode}
}
func (h Handler) Handle(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": transport.Message(c, i18n.MsgSystemTaskIDRequired),
		})
		return
	}

	task, err := h.store.GetSystemTaskByTaskID(taskID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": transport.Message(c, i18n.MsgSystemTaskNotFound),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dto.TaskResponse(task, h.decode),
	})
}
