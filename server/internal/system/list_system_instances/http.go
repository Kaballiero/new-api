package list_system_instances

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/gin-gonic/gin"
)

type Store interface {
	ListSystemInstances() ([]domain.Instance, error)
}
type Handler struct {
	store  Store
	now    func() int64
	decode func(string) any
}

func NewHandler(store Store, now func() int64, decode func(string) any) Handler {
	return Handler{store: store, now: now, decode: decode}
}
func (h Handler) Handle(c *gin.Context) {
	instances, err := h.store.ListSystemInstances()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	now := h.now()
	responses := make([]SystemInstanceResponse, 0, len(instances))
	for _, v := range instances {
		responses = append(responses, SystemInstanceResponse{
			NodeName:          v.NodeName,
			Status:            domain.InstanceStatus(v.LastSeenAt, now),
			StaleAfterSeconds: domain.SystemInstanceStaleAfterSeconds,
			StartedAt:         v.StartedAt,
			LastSeenAt:        v.LastSeenAt,
			Info:              h.decode(v.Info),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responses,
	})
}
