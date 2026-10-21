package delete_vendor

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
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
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsgStatusCode(c, http.StatusBadRequest, "invalid_params", "invalid id")
		return
	}
	input := Input{ID: id}
	audit := func(input Input, result Output) { h.audit(c, input, result) }
	_, err = NewUsecase(h.store, h.now, h.refresh, audit).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
