package get_oauth_bindings

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/gin-gonic/gin"
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
		common.ApiErrorI18n(c, i18n.MsgIdentityNotLoggedIn)
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
	bindings, err := ReadAsAdmin(id, h.admin(c))
	if errors.Is(err, domain.ErrTargetForbidden) {
		common.ApiErrorMsg(c, "no permission")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": bindings})
}
