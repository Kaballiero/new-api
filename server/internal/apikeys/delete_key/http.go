package delete_key

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	tokenhttp "github.com/QuantumNous/new-api/internal/adapter/apikeyhttp"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct {
	reader Store
	audit  func(*gin.Context) map[string]any
}

func NewHTTP(reader Store, audit func(*gin.Context) map[string]any) HTTP {
	return HTTP{reader: reader, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	_, failure := NewUsecase(h.reader, func() map[string]any { return h.audit(c) }, func() { common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true) }).Execute(Input{ID: id, UserID: userId})
	if failure != nil {
		tokenhttp.WriteFailure(c, failure)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
