package update_vendor

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	store   Store
	now     func() int64
	refresh func()
	audit   func(*gin.Context, int, string)
}

func NewHTTP(store Store, now func() int64, refresh func(), audit func(*gin.Context, int, string)) HTTP {
	return HTTP{store: store, now: now, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var value Request
	if err := c.ShouldBindJSON(&value); err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", err.Error())
		return
	}
	audit := func(id int, name string) { h.audit(c, id, name) }
	out, err := NewUsecase(h.store, h.now, h.refresh, audit).Execute(Input{Value: domain.Vendor(value)})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
