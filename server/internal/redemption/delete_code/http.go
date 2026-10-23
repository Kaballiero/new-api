package delete_code

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/redemption/transport"
)

type HTTP struct {
	usecase Usecase
}

func NewHTTP(usecase Usecase) HTTP {
	return HTTP{usecase: usecase}
}

func (h HTTP) Handle(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := h.usecase.Execute(id); err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
