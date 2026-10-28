package list_task_plugin_versions

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"

	"github.com/gin-gonic/gin"
)

type Reader interface {
	ListTaskPluginVersions(string) ([]domain.Plugin, error)
}
type Handler struct{ reader Reader }

func NewHandler(reader Reader) Handler { return Handler{reader: reader} }
func (h Handler) ServeHTTP(c *gin.Context) {
	v, e := h.reader.ListTaskPluginVersions(c.Param("key"))
	if e != nil {
		common.ApiError(c, e)
		return
	}
	common.ApiSuccess(c, v)
}
