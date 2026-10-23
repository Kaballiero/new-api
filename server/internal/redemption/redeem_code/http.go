package redeem_code

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
)

// Admission holds a process-local user lock across binding, the transaction and post-commit effects.
type Admission interface {
	Acquire(userID int) (release func(), acquired bool)
}

type HTTP struct {
	usecase Usecase
	// compliant checks payment admission before the user lock or transaction.
	compliant func() bool
	admission Admission
	// failed logs the generic redemption failure outside the completed transaction.
	failed func(c *gin.Context, key string, userID int, err error)
}

func NewHTTP(usecase Usecase, compliant func() bool, admission Admission, failed func(c *gin.Context, key string, userID int, err error)) HTTP {
	return HTTP{usecase: usecase, compliant: compliant, admission: admission, failed: failed}
}

func (h HTTP) Handle(c *gin.Context) {
	if !h.compliant() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}
	id := c.GetInt("id")
	release, ok := h.admission.Acquire(id)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer release()
	req := topUpRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := h.usecase.Execute(Input{Key: req.Key, UserID: id})
	if err != nil {
		// Preserve the generic error; detailed failures must not reveal code state to the caller.
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		h.failed(c, req.Key, id, err)
		return
	}
	common.ApiSuccess(c, quota)
}
