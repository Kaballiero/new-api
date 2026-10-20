package clear_binding

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
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
	result := NewUsecase(h.factory(c)).Execute(Input{ID: id, BindingType: bindingType})
	if result.Cause != nil {
		common.ApiError(c, result.Cause)
		return
	}
	if result.Forbidden {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success"})
}
