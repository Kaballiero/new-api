package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"sync"

	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// accountsHTTP builds the account handlers once; settings, the database and
// the request are read per call by the injected functions.
var accountsHTTP = sync.OnceValue(func() app.AccountsHTTP {
	return app.NewAccountsHTTP(app.AccountsEffects{ManageAudit: recordManageAuditFor, SecurityAudit: recordUserSecurityAudit, SecurityError: writeSecurityOperationError, PaymentCompliance: func(c *gin.Context) bool {
		if !operation_setting.IsPaymentComplianceConfirmed() {
			common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return false
		}
		return true
	}, GetAPIError: writeGetAPIError, QuotaAudit: func(c *gin.Context) adjust_quota.Audit { return accountQuotaAudit{context: c} }, Sidebar: generateDefaultSidebarConfig})
})
