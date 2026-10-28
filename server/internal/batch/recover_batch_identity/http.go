package recover_batch_identity

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/transport"

	"github.com/gin-gonic/gin"
)

type Audit func(*gin.Context, string, map[string]any, map[string]any, int, bool)
type Handler struct {
	usecase Usecase
	audit   Audit
}

func NewHandler(u Usecase, audit Audit) Handler { return Handler{u, audit} }
func (h Handler) Handle(c *gin.Context) {
	var request struct {
		UpstreamID string `json:"upstream_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		transport.Error(c, &domain.RuleError{Kind: domain.InvalidRecoveryIdentity})
		return
	}
	if request.UpstreamID == "" || len(request.UpstreamID) > 191 {
		transport.Error(c, &domain.RuleError{Kind: domain.InvalidRecoveryIdentity})
		return
	}
	outcome := "recovered"
	result, err := h.usecase.Execute(c.Request.Context(), Input{c.Param("id"), request.UpstreamID})
	defer func() {
		status := c.Writer.Status()
		if status == 404 {
			return
		}
		previous := result.Previous
		h.audit(c, "batch.recover", map[string]any{"id": c.Param("id"), "outcome": outcome}, map[string]any{"upstream_id": request.UpstreamID, "user_id": previous.UserID, "token_id": previous.TokenID, "channel_id": previous.ChannelID, "previous_submit_state": previous.SubmitState, "previous_status": previous.Status, "previous_billing_status": previous.BillingStatus}, status, status == 200)
	}()
	if err != nil {
		outcome = transport.Error(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "id": result.PublicID})
}
