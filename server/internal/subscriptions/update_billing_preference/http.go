package update_billing_preference

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
)

type BillingPreferenceRequest struct {
	BillingPreference string `json:"billing_preference"`
}
type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	var req BillingPreferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		transport.InvalidParameters(c)
		return
	}
	preference, err := h.usecase.Execute(Input{UserID: c.GetInt("id"), Preference: req.BillingPreference})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"billing_preference": preference})
}
