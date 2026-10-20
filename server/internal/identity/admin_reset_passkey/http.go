package admin_reset_passkey

import (
	"github.com/QuantumNous/new-api/common"
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
	if err != nil {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	result, err := NewUsecase(h.factory(c)).Execute(id)
	if err != nil {
		identityhttp.WriteSecurityOperationError(c, err)
		return
	}
	switch result.Issue {
	case "forbidden":
		common.ApiErrorMsg(c, "no permission")
		return
	case "missing":
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "该用户尚未绑定 Passkey"})
		return
	}
	h.audit(c, result.ID, "user.reset_passkey", map[string]any{"username": result.Username, "id": result.ID})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Passkey 已重置"})
}
