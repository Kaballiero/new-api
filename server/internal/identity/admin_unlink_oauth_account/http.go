package admin_unlink_oauth_account

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ factory func(*gin.Context) Operation }

func NewHTTP(factory func(*gin.Context) Operation) *HTTP { return &HTTP{factory: factory} }
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}
	err = NewUsecase(h.factory(c)).Execute(Input{UserID: id})
	switch {
	case errors.Is(err, domain.ErrTargetForbidden):
		common.ApiErrorMsg(c, "no permission")
		return
	case errors.Is(err, domain.ErrProviderIDInvalid):
		common.ApiErrorMsg(c, "invalid provider id")
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success"})
}
