package delete_keys_batch

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct {
	reader Store
	audit  func(*gin.Context, []int) map[string]any
}

func NewHTTP(reader Store, audit func(*gin.Context, []int) map[string]any) HTTP {
	return HTTP{reader: reader, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	tokenBatch := Request{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := NewUsecase(h.reader, func(ids []int) map[string]any { return h.audit(c, ids) }, func() { common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true) }).Execute(tokenBatch.Ids, c.GetInt("id"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result.Count,
	})
}
