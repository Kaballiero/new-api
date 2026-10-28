package transfer_affiliate_quota

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	scenario   Usecase
	compliance func(*gin.Context) bool
}

func NewHTTP(loader AffiliateAccountLoader, compliance func(*gin.Context) bool) *HTTP {
	return &HTTP{scenario: NewUsecase(loader), compliance: compliance}
}

type affiliateQuotaInput struct{ context *gin.Context }

func (input affiliateQuotaInput) ReadQuota() (int, error) {
	tran := TransferAffQuotaRequest{}
	if err := input.context.ShouldBindJSON(&tran); err != nil {
		return 0, err
	}
	return tran.Quota, nil
}

func (h *HTTP) Handle(c *gin.Context) {
	if !h.compliance(c) {
		return
	}

	id := c.GetInt("id")
	if err := h.scenario.Execute(id, affiliateQuotaInput{context: c}); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccessI18n(c, i18n.MsgUserTransferSuccess, nil)
}
