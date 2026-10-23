package list_key_logs

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/usage/transport"
)

type Handler struct {
	usecase Usecase
}

func NewHandler(usecase Usecase) Handler {
	return Handler{usecase: usecase}
}

func (h Handler) Handle(c *gin.Context) {
	tokenID := c.GetInt("token_id")
	if tokenID == 0 {
		c.JSON(200, gin.H{"success": false, "message": transport.Translate(c, i18n.MsgUsageInvalidToken)})
		return
	}
	logs, err := h.usecase.Execute(tokenID)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": logs})
}
