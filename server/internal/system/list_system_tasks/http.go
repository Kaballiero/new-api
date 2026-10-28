package list_system_tasks

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/internal/system/dto"

	"github.com/gin-gonic/gin"
)

type Store interface {
	ListSystemTasks(int) ([]*domain.SystemTask, error)
}
type Handler struct {
	store  Store
	decode func(string) any
}

func NewHandler(store Store, decode func(string) any) Handler {
	return Handler{store: store, decode: decode}
}
func (h Handler) Handle(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))

	tasks, err := h.store.ListSystemTasks(limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	responses := make([]dto.SystemTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		responses = append(responses, dto.TaskResponse(task, h.decode))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responses,
	})
}
