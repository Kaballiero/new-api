package get_rankings

import (
	"net/http"

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
	result, err := h.usecase.Execute(c.DefaultQuery("period", "week"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": transport.Message(c, err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
