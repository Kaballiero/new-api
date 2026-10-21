package apply_vendor_operation

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store   Store
	now     func() int64
	refresh func()
	audit   func(*gin.Context, Input, Output)
}

func NewHTTP(store Store, now func() int64, refresh func(), audit func(*gin.Context, Input, Output)) HTTP {
	return HTTP{store: store, now: now, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		transport.WriteError(c, domain.RejectVendorChange(err, false))
		return
	}
	audit := func(input Input, result Output) { h.audit(c, input, result) }
	out, err := NewUsecase(h.store, h.now, h.refresh, audit).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
