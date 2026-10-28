package fetch_task_artifact

import (
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/transport"

	"github.com/gin-gonic/gin"
)

type Delivery struct {
	Proxy      func(*gin.Context, domain.MediaTask, *domain.MediaRequest) error
	Stored     func(*gin.Context, *domain.Task, *domain.StoredArtifactRef) error
	LogError   func(*gin.Context, error)
	Capability func(*gin.Context) bool
}
type Handler struct {
	usecase  Usecase
	delivery Delivery
}

func NewHandler(usecase Usecase, delivery Delivery) Handler {
	return Handler{usecase: usecase, delivery: delivery}
}
func (h Handler) Handle(c *gin.Context) {
	capability := h.delivery.Capability(c)
	result, err := h.usecase.Execute(Input{
		TaskID:      c.Param("key"),
		ArtifactKey: c.Param("artifact_key"),
		Access:      transport.ReadTaskAccess(c, capability),
		Client:      domain.MediaClientRequest{Method: c.Request.Method, Headers: transport.MediaClientHeaders(c.Request.Header)},
	})
	if err != nil {
		transport.WriteTaskError(c, capability, err)
		return
	}
	if result.Stored != nil {
		if err := h.delivery.Stored(c, result.Task, result.Stored); err != nil && result.RequiredStorage && !c.Writer.Written() {
			transport.WriteTaskError(c, capability, domain.ErrArtifactStorageUnavailable)
		}
		return
	}
	if err := h.delivery.Proxy(c, domain.MediaTask{TaskID: result.Task.TaskID, ChannelID: result.Task.ChannelId}, result.Descriptor); err != nil {
		transport.WriteMediaError(c, capability, err, func(err error) { h.delivery.LogError(c, err) })
	}
}
