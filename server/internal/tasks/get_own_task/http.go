package get_own_task

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/transport"
)

type Handler struct{ usecase Usecase }

func NewHandler(usecase Usecase) Handler { return Handler{usecase: usecase} }
func (h Handler) Handle(c *gin.Context) {
	result, err := h.usecase.Execute(Input{UserID: c.GetInt("id"), TaskID: c.Param("key")})
	if err != nil {
		if errors.Is(err, domain.ErrTaskQuery) {
			transport.WriteVideoError(c, http.StatusInternalServerError, "server_error", i18n.T(c, i18n.MsgTasksQueryTaskFailed))
		} else {
			transport.WriteVideoError(c, http.StatusNotFound, "invalid_request_error", i18n.T(c, i18n.MsgTasksTaskNotFound))
		}
		return
	}
	task := result.Task
	response := gin.H{
		"task_id":     task.TaskID,
		"platform":    task.Platform,
		"status":      task.Status,
		"progress":    task.Progress,
		"fail_reason": result.FailReason,
		"created_at":  result.CreatedAt,
		"finished_at": task.FinishTime,
	}
	if result.Reconciliation != nil {
		response["reconciliation"] = result.Reconciliation
	}
	c.JSON(http.StatusOK, response)
}
