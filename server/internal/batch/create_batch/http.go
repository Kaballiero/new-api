package create_batch

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/transport"

	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	p := domain.PreparedRequest{Request: c.MustGet("batch_create_request").(domain.CreateRequest),
		PublicID: c.GetString("batch_public_id"), KeyHash: c.GetString("batch_key_hash"), RequestHash: c.GetString("batch_request_hash")}
	r, err := h.usecase.Execute(c, Input{p})
	if err != nil {
		transport.Error(c, err)
		return
	}
	status := 202
	if r.Duplicate {
		status = 200
	}
	c.JSON(status, dto.Job(*r.Job).WithLanguage(i18n.GetLangFromContext(c)))
}
