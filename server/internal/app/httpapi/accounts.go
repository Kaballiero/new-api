package httpapi

import (
	"sync"

	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	auditrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/app"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	usagestorage "github.com/QuantumNous/new-api/internal/app/usagestorage"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	audittransport "github.com/QuantumNous/new-api/internal/audit/transport"
	subscriptiontransport "github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// Accounts builds the account handlers once; settings, the database and
// the request are read per call by the injected functions.
var Accounts = sync.OnceValue(func() app.AccountsHTTP {
	return app.NewAccountsHTTP(app.AccountsEffects{ManageAudit: recordManageAuditFor, SecurityAudit: recordUserSecurityAudit, SecurityError: identityErrors.WriteError, PaymentCompliance: func(c *gin.Context) bool {
		return subscriptiontransport.CheckCompliance(c, operation_setting.IsPaymentComplianceConfirmed)
	}, GetAPIError: func(c *gin.Context, err error) { app.GetAPIErrors().Write(c, err) }, QuotaAudit: func(c *gin.Context) adjust_quota.Audit { return accountQuotaAudit{context: c} }, Sidebar: generateDefaultSidebarConfig})
})

type accountQuotaAudit struct {
	context *gin.Context
}

func (a accountQuotaAudit) RecordRecipientAdjustment(adjustment *adjust_quota.UserQuotaAdjustment, action string, params map[string]any) {
	c := a.context
	operation := auditrecord.AuditOperation{Action: action, Params: params}
	usagestorage.Writer().RecordLogWithAdminInfo(adjustment.UserID, usagerecord.LogTypeTopup, auditdomain.OperationContent(action, params), quotaOperatorInfo(c), &operation, c)
}

func (a accountQuotaAudit) RecordOperatorAdjustment(action string, params map[string]any, success bool) {
	auditapp.Recording().Quota(a.context, action, params, success)
}

func quotaOperatorInfo(c *gin.Context) *auditrecord.AuditAdminInfo {
	operator := audittransport.Operator(c)
	return &auditrecord.AuditAdminInfo{AdminID: operator.ID, AdminUsername: operator.Username, AdminRole: operator.Role, AuthMethod: operator.AuthMethod}
}
