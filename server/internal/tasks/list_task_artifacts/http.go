package list_task_artifacts

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/transport"
)

type Handler struct {
	usecase    Usecase
	capability func(*gin.Context) bool
}

func NewHandler(usecase Usecase, capability func(*gin.Context) bool) Handler {
	return Handler{usecase: usecase, capability: capability}
}
func (h Handler) Handle(c *gin.Context)    { h.handle(c, false) }
func (h Handler) Dashboard(c *gin.Context) { h.handle(c, true) }
func (h Handler) handle(c *gin.Context, dashboard bool) {
	taskID := c.Param("key")
	if dashboard {
		taskID = c.Param("task_id")
	}
	capability := h.capability(c)
	result, err := h.usecase.Execute(Input{TaskID: taskID, Access: transport.ReadTaskAccess(c, capability), OwnOnly: !dashboard})
	if err != nil {
		transport.WriteTaskError(c, capability, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response := gin.H{"task_id": result.TaskID, "artifacts": result.Artifacts}
	if result.LegacyContentURL != "" {
		response["legacy_content_url"] = result.LegacyContentURL
	}
	if dashboard {
		common.ApiSuccess(c, response)
		return
	}
	c.JSON(http.StatusOK, response)
}
