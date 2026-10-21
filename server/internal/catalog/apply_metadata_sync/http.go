package apply_metadata_sync

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	store   Store
	source  Source
	now     func() int64
	refresh func()
	audit   func(*gin.Context, Output)
}

func NewHTTP(store Store, source Source, now func() int64, refresh func(), audit func(*gin.Context, Output)) HTTP {
	return HTTP{store: store, source: source, now: now, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || len(input.Selections) == 0 || input.SourceVersion == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Preview and select metadata changes before applying"})
		return
	}
	audit := func(result Output) { h.audit(c, result) }
	out, err := NewUsecase(h.store, h.source, h.now, h.refresh, audit).Execute(c.Request.Context(), input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}
