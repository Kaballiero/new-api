package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// BRIDGE: subscription management and checkout controllers; remove when their compliance admission is extracted.
func requirePaymentCompliance(c *gin.Context) bool {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return false
	}
	return true
}
func ConfirmPaymentCompliance(c *gin.Context) { paymentsHTTP().Compliance.Handle(c) }
