package get_own_usage_flow

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	query, ok := transport.ReadFlowRange(c)
	if !ok {
		return
	}
	query.UserID = c.GetInt("id")
	rows, err := h.usecase.Execute(Input{Query: query})
	if errors.Is(err, domain.ErrQuotaSpan) {
		c.JSON(200, gin.H{"success": false, "message": transport.Message(c, domain.ErrQuotaSpan)})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": rows})
}
