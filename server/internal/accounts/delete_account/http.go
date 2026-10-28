package delete_account

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ factory func(*gin.Context) Operation }

func NewHTTP(factory func(*gin.Context) Operation) *HTTP { return &HTTP{factory: factory} }
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
		return
	}
	actorRole := c.GetInt("role")
	if err := NewUsecase(h.factory(c)).Execute(Input{ID: id, ActorRole: actorRole}); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
