package confirm_batch_purchase

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{u} }
func (h Handler) Handle(c *gin.Context) {
	var in Input
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	if err := common.DecodeJson(c.Request.Body, &in); err != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.InvalidPurchaseEvidence})
		return
	}
	in.PublicID = c.Param("id")
	if err := h.usecase.Execute(c, in); err != nil {
		transport.Error(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "id": c.Param("id")})
}
