package list_prefill_groups

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type Reader interface {
	List(string) ([]domain.PrefillGroup, error)
}
type HTTP struct{ reader Reader }

func NewHTTP(reader Reader) HTTP { return HTTP{reader: reader} }
func (h HTTP) Handle(c *gin.Context) {
	groups, err := h.reader.List(c.Query("type"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var out []transport.PrefillGroup
	if groups != nil {
		out = make([]transport.PrefillGroup, len(groups))
		for i, v := range groups {
			out[i] = transport.PrefillResponse(v)
		}
	}
	common.ApiSuccess(c, out)
}
