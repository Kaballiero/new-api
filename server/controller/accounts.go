package controller

import (
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/app"
	subscriptiontransport "github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// accountsHTTP builds the account handlers once; settings, the database and
// the request are read per call by the injected functions.
var accountsHTTP = sync.OnceValue(func() app.AccountsHTTP {
	return app.NewAccountsHTTP(app.AccountsEffects{ManageAudit: recordManageAuditFor, SecurityAudit: recordUserSecurityAudit, SecurityError: writeSecurityOperationError, PaymentCompliance: func(c *gin.Context) bool {
		return subscriptiontransport.CheckCompliance(c, operation_setting.IsPaymentComplianceConfirmed)
	}, GetAPIError: writeGetAPIError, QuotaAudit: func(c *gin.Context) adjust_quota.Audit { return accountQuotaAudit{context: c} }, Sidebar: generateDefaultSidebarConfig})
})
