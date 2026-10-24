package fetch_video_content

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/transport"
)

type Handler struct {
	usecase    Usecase
	proxy      func(*gin.Context, domain.MediaTask, *domain.MediaRequest) error
	capability func(*gin.Context) bool
	logError   func(*gin.Context, error)
}

func NewHandler(
	usecase Usecase,
	proxy func(*gin.Context, domain.MediaTask, *domain.MediaRequest) error,
	capability func(*gin.Context) bool,
	logError func(*gin.Context, error),
) Handler {
	return Handler{usecase: usecase, proxy: proxy, capability: capability, logError: logError}
}
func (h Handler) Handle(c *gin.Context) {
	capability := h.capability(c)
	result, err := h.usecase.Execute(c.Request.Context(), Input{
		TaskID: c.Param("task_id"),
		Access: transport.ReadTaskAccess(c, capability),
		Client: domain.MediaClientRequest{Method: c.Request.Method, Headers: transport.MediaClientHeaders(c.Request.Header)},
	})
	if err != nil {
		var incomplete *domain.TaskIncompleteError
		switch {
		case errors.Is(err, domain.ErrVideoTaskIDRequired):
			transport.WriteVideoError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		case errors.Is(err, domain.ErrTaskQuery):
			transport.WriteVideoError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		case errors.Is(err, domain.ErrTaskNotFound):
			transport.WriteVideoError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		case errors.As(err, &incomplete):
			message := fmt.Sprintf("Task is not completed yet, current status: %s", incomplete.Status)
			transport.WriteVideoError(c, http.StatusBadRequest, "invalid_request_error", message)
		default:
			transport.WriteTaskError(c, capability, err)
		}
		return
	}
	if err := h.proxy(c, domain.MediaTask{TaskID: result.Task.TaskID, ChannelID: result.Task.ChannelId}, result.Descriptor); err != nil {
		transport.WriteMediaError(c, capability, err, func(err error) { h.logError(c, err) })
	}
}
