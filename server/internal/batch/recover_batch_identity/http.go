package recover_batch_identity

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
	var request struct {
		UpstreamID string `json:"upstream_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.InvalidRecoveryIdentity})
		return
	}
	id, err := h.usecase.Execute(c.Request.Context(), Input{c.Param("id"), request.UpstreamID})
	if err != nil {
		transport.Error(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "id": id})
}
