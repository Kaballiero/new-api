package confirm_compliance

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type PaymentComplianceRequest struct {
	Confirmed bool `json:"confirmed"`
}

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	if c.GetBool("use_access_token") {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": transport.Message(c, domain.ErrSessionOnly)})
		return
	}
	var req PaymentComplianceRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		transport.WriteError(c, domain.ErrInvalidParameters)
		return
	}
	result, err := h.usecase.Execute(transport.Context(c), Input{Confirmed: req.Confirmed, UserID: c.GetInt("id"), ClientIP: c.ClientIP()})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"confirmed": true, "terms_version": result.Terms, "confirmed_at": result.Time, "confirmed_by": result.UserID})
}
