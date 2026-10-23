package admin_list_user_usage_data

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	query := transport.ReadQuotaQuery(c)
	result, err := h.usecase.Execute(query)
	if err != nil {
		transport.WriteQuotaError(c, err)
		return
	}
	if query.Granularity != "" {
		common.ApiSuccess(c, result.Bucketed)
		return
	}
	common.ApiSuccess(c, result.Legacy)
}
