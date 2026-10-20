package get_oauth_bindings

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct {
	reader Reader
	admin  func(*gin.Context) AdminReader
}

func NewHTTP(reader Reader, admin func(*gin.Context) AdminReader) *HTTP {
	return &HTTP{reader: reader, admin: admin}
}
func (h *HTTP) Self(c *gin.Context) {
	id := c.GetInt("id")
	if id == 0 {
		common.ApiErrorMsg(c, "未登录")
		return
	}
	bindings, err := NewUsecase(h.reader).Execute(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": bindings})
}
func (h *HTTP) Admin(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	result := ReadAsAdmin(id, h.admin(c))
	if result.Cause != nil {
		common.ApiError(c, result.Cause)
		return
	}
	if result.Forbidden {
		common.ApiErrorMsg(c, "no permission")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result.Bindings})
}
