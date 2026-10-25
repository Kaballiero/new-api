package fetch_batch_results

import (
	"errors"
	"io"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	job, err := h.usecase.Execute(c, Input{UserID: c.GetInt("id"), PublicID: c.Param("id")})
	if err != nil {
		transport.Error(c, err)
		return
	}
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Cache-Control", "no-store")
	var cursor int64
	for {
		item, err := h.usecase.NextResult(job.ID, cursor)
		if errors.Is(err, domain.ErrNotFound) {
			return
		}
		if err != nil {
			c.Abort()
			return
		}
		cursor = item.ID
		if _, err := io.WriteString(c.Writer, item.Result+"\n"); err != nil {
			return
		}
		c.Writer.Flush()
		if c.Request.Context().Err() != nil {
			return
		}
	}
}
