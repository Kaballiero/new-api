package controller

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// accountsHTTP builds the account handlers once; settings, the database and
// the request are read per call by the injected functions.
var accountsHTTP = sync.OnceValue(func() app.AccountsHTTP {
	return app.NewAccountsHTTP(app.AccountsEffects{ManageAudit: recordManageAuditFor, SecurityAudit: recordUserSecurityAudit, SecurityError: writeSecurityOperationError, PaymentCompliance: requirePaymentCompliance, GetAPIError: writeGetAPIError, QuotaAudit: func(c *gin.Context) adjust_quota.Audit { return accountQuotaAudit{context: c} }, Sidebar: generateDefaultSidebarConfig})
})
