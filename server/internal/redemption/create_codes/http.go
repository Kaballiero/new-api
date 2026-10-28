package create_codes

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
	"github.com/QuantumNous/new-api/internal/redemption/dto"
	"github.com/QuantumNous/new-api/internal/redemption/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase Usecase
	// compliant checks payment admission before binding or persistence.
	compliant func() bool
	// audit records complete batch success after all inserts, outside their transactions.
	audit func(c *gin.Context, input Input)
	// failed logs an insert failure after that persistence operation returns.
	failed func(err error)
}

func NewHTTP(usecase Usecase, compliant func() bool, audit func(c *gin.Context, input Input), failed func(err error)) HTTP {
	return HTTP{usecase: usecase, compliant: compliant, audit: audit, failed: failed}
}

func (h HTTP) Handle(c *gin.Context) {
	if !h.compliant() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}
	redemption := dto.Redemption{}
	if err := c.ShouldBindJSON(&redemption); err != nil {
		common.ApiError(c, err)
		return
	}
	input := Input{IssuerID: c.GetInt("id"), Name: redemption.Name, Count: redemption.Count, Quota: redemption.Quota,
		ExpiredTime: redemption.ExpiredTime}
	result, err := h.usecase.Execute(input)
	if err != nil {
		if errors.Is(err, domain.ErrCreate) {
			h.failed(err)
			c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgRedemptionCreateFailed), "data": result.Keys})
			return
		}
		transport.WriteError(c, err)
		return
	}
	h.audit(c, input)
	common.ApiSuccess(c, result.Keys)
}
