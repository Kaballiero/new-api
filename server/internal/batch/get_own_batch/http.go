package get_own_batch

import (
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/transport"
	"github.com/gin-gonic/gin"
)

type Reader interface {
	GetBatchJob(int, string) (*domain.Job, error)
}
type Handler struct{ reader Reader }

func NewHandler(r Reader) Handler { return Handler{r} }
func (h Handler) Handle(c *gin.Context) {
	job, err := h.reader.GetBatchJob(c.GetInt("id"), c.Param("id"))
	if err != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.NotFound})
		return
	}
	c.JSON(200, dto.Job(*job))
}
