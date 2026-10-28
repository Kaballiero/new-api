package delete_codes

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/redemption/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase Usecase
	// audit records deletion after the persistence operation, outside its transaction.
	audit func(c *gin.Context, ids []int, count int64)
}

func NewHTTP(usecase Usecase, audit func(c *gin.Context, ids []int, count int64)) HTTP {
	return HTTP{usecase: usecase, audit: audit}
}

func (h HTTP) Handle(c *gin.Context) {
	var request struct {
		Ids []int `json:"ids" binding:"required,min=1,max=1000,dive,gt=0"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	count, err := h.usecase.Execute(request.Ids)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit(c, request.Ids, count)
	common.ApiSuccess(c, count)
}
