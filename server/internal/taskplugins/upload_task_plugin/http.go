package upload_task_plugin

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"github.com/gin-gonic/gin"
)

type taskPluginUploadRequest struct {
	Source       string `json:"source" binding:"required"`
	Enabled      *bool  `json:"enabled"`
	Remark       string `json:"remark"`
	Force        bool   `json:"force"`
	SourceSha256 string `json:"sourceSha256"`
	// Icon carries the sidecar icon.svg / icon.png as a data URI. It is optional
	// and stored separately from the source so the JavaScript stays readable.
	Icon string `json:"icon"`
}
type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	var request taskPluginUploadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	input := Input{Source: request.Source,
		Enabled:      request.Enabled,
		Remark:       request.Remark,
		Force:        request.Force,
		SourceSha256: request.SourceSha256,
		Icon:         request.Icon}
	value, err := h.usecase.Execute(transport.Context(c), input)
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, value)
}
