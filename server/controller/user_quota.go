package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/model"
)

// BRIDGE: accountQuotaAudit connects the existing audit implementation with live request metadata.
type accountQuotaAudit struct {
	context *gin.Context
}

func (a accountQuotaAudit) RecordRecipientAdjustment(adjustment *adjust_quota.UserQuotaAdjustment, action string, params map[string]any) {
	c := a.context
	operation := model.AuditOperation{Action: action, Params: params}
	model.RecordLogWithAdminInfo(adjustment.UserID, model.LogTypeTopup, auditContentEN(action, params), auditOperatorInfo(c), &operation, c)
}

func (a accountQuotaAudit) RecordOperatorAdjustment(action string, params map[string]any, success bool) {
	auditRecording().Quota(a.context, action, params, success)
}
