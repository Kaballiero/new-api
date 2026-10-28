package reveal_key

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	reader Reader
	audit  func(*gin.Context) map[string]any
}

func NewHTTP(reader Reader, audit func(*gin.Context) map[string]any) HTTP {
	return HTTP{reader: reader, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	userId := c.GetInt("id")
	token, err := NewUsecase(h.reader, func() map[string]any { return h.audit(c) }, func() { common.SetContextKey(c, constant.ContextKeyTokenAuditSucceeded, true) }).Execute(Input{ID: id, UserID: userId})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, TokenKeyResponse{Key: token.Key})
}
