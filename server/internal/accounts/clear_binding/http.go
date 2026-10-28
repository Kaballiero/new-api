package clear_binding

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	factory func(*gin.Context) AdminOperation
}

func NewHTTP(factory func(*gin.Context) AdminOperation) *HTTP { return &HTTP{factory: factory} }
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	bindingType := strings.ToLower(strings.TrimSpace(c.Param("binding_type")))
	if bindingType == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := NewUsecase(h.factory(c)).Execute(Input{ID: id, BindingType: bindingType}); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success"})
}
