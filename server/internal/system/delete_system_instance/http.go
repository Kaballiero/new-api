package delete_system_instance

import (
	"strings"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/system/transport"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type Store interface {
	DeleteStaleSystemInstance(string, int64) (bool, error)
}
type Handler struct {
	store Store
	now   func() int64
}

func NewHandler(store Store, now func() int64) Handler { return Handler{store: store, now: now} }
func (h Handler) Handle(c *gin.Context) {
	nodeName := c.Param("node_name")
	if strings.TrimSpace(nodeName) == "" {
		common.ApiErrorMsg(c, transport.Message(c, i18n.MsgSystemNodeRequired))
		return
	}

	deleted, err := h.store.DeleteStaleSystemInstance(nodeName, h.now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !deleted {
		common.ApiErrorMsg(c, transport.Message(c, i18n.MsgSystemInstanceNotStale))
		return
	}

	common.ApiSuccess(c, gin.H{
		"deleted_count": 1,
	})
}
