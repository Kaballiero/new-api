package fetch_video_content

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/transport"

	"github.com/gin-gonic/gin"
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
			transport.WriteVideoError(c, http.StatusBadRequest, "invalid_request_error", i18n.T(c, i18n.MsgTasksTaskIDRequired))
		case errors.Is(err, domain.ErrTaskQuery):
			transport.WriteVideoError(c, http.StatusInternalServerError, "server_error", i18n.T(c, i18n.MsgTasksQueryTaskFailed))
		case errors.Is(err, domain.ErrTaskNotFound):
			transport.WriteVideoError(c, http.StatusNotFound, "invalid_request_error", i18n.T(c, i18n.MsgTasksTaskNotFound))
		case errors.As(err, &incomplete):
			message := i18n.T(c, i18n.MsgTasksTaskNotCompleted, map[string]any{"Status": incomplete.Status})
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
