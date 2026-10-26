package delete_prefill_group

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type Store interface{ Delete(int) error }
type HTTP struct{ store Store }

func NewHTTP(store Store) HTTP { return HTTP{store: store} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := h.store.Delete(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
