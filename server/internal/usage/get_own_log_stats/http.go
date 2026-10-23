package get_own_log_stats

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/internal/usage/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	query := transport.ReadLogQuery(c)
	result, err := h.usecase.Execute(Input{Query: query, Username: c.GetString("username")})
	if err != nil {
		transport.WriteLogError(c, err)
		return
	}
	common.ApiSuccess(c, dto.Stat(result))
}
