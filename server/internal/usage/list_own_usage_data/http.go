package list_own_usage_data

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	query := transport.ReadQuotaQuery(c)
	query.UserID = c.GetInt("id")
	if raw := c.Query("token_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			common.ApiErrorMsgStatusCode(c, 400, "invalid_token_id", transport.Message(c, domain.ErrUsageInvalidTokenID))
			return
		}
		query.TokenID = id
	}
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
