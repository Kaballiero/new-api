package admin_unlink_oauth_account

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ factory func(*gin.Context) Operation }

func NewHTTP(factory func(*gin.Context) Operation) *HTTP { return &HTTP{factory: factory} }
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	result := NewUsecase(h.factory(c)).Execute(Input{UserID: id})
	if result.Cause != nil {
		common.ApiError(c, result.Cause)
		return
	}
	if result.Forbidden {
		common.ApiErrorMsg(c, "no permission")
		return
	}
	if result.InvalidProvider {
		common.ApiErrorMsg(c, "invalid provider id")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success"})
}
