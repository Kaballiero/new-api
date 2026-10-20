package controller

import (
	"github.com/QuantumNous/new-api/internal/accounts/manage_account"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/gin-gonic/gin"
)

// NewAccountsHTTP composes compatibility entrypoints without global initialization.
func NewAccountsHTTP() app.AccountsHTTP {
	return app.NewAccountsHTTP(app.AccountsEffects{ManageAudit: recordManageAuditFor, SecurityAudit: recordUserSecurityAudit, SecurityError: writeSecurityOperationError, PaymentCompliance: requirePaymentCompliance, GetAPIError: writeGetAPIError, Quota: func(c *gin.Context, r manage_account.ManageRequest) { manageUserQuota(c, r) }, Sidebar: generateDefaultSidebarConfig})
}
