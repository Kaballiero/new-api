package admin_disable_twofa

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct {
	factory func(*gin.Context) Operation
	audit   func(*gin.Context, int, string, map[string]any)
}

func NewHTTP(factory func(*gin.Context) Operation, audit func(*gin.Context, int, string, map[string]any)) *HTTP {
	return &HTTP{factory: factory, audit: audit}
}
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "用户ID格式错误"})
		return
	}
	result, err := NewUsecase(h.factory(c)).Execute(id)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	switch result.Issue {
	case "missing":
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	case "forbidden":
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无权操作同级或更高级用户的2FA设置"})
		return
	case "disabled":
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "用户未启用2FA"})
		return
	}
	h.audit(c, id, "user.2fa_disable", nil)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "用户2FA已被强制禁用"})
}
