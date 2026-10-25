package sweep_stale_instances

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type Store interface{ DeleteStaleSystemInstances(int64) (int64, error) }
type Handler struct {
	store Store
	now   func() int64
}

func NewHandler(store Store, now func() int64) Handler { return Handler{store: store, now: now} }
func (h Handler) Handle(c *gin.Context) {
	deletedCount, err := h.store.DeleteStaleSystemInstances(h.now())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"deleted_count": deletedCount,
	})
}
