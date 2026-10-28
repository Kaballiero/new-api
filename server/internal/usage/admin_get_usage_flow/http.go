package admin_get_usage_flow

import (
	"github.com/QuantumNous/new-api/common"
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
	query, ok := transport.ReadFlowRange(c)
	if !ok {
		return
	}
	query.Username = c.Query("username")
	rows, err := h.usecase.Execute(Input{Query: query, Audience: transport.Audience(c)})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": rows})
}
