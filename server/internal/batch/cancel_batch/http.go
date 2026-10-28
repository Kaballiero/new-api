package cancel_batch

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	r, e := h.usecase.Execute(c, Input{c.GetInt("id"), c.Param("id")})
	if e != nil {
		transport.Error(c, e)
		return
	}
	if r.Terminal {
		c.JSON(200, dto.Job(*r.Job).WithLanguage(i18n.GetLangFromContext(c)))
		return
	}
	c.JSON(202, gin.H{"id": r.Job.PublicID, "status": "cancelling"})
}
