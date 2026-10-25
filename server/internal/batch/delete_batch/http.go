package delete_batch

import (
	"github.com/QuantumNous/new-api/internal/batch/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	id, err := h.usecase.Execute(c, Input{c.GetInt("id"), c.Param("id")})
	if err != nil {
		transport.Error(c, err)
		return
	}
	c.JSON(200, gin.H{"id": id, "deleted": true})
}
