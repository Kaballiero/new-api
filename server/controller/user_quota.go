package controller

import (
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/app"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func manageUserQuota(c *gin.Context, req ManageRequest) {
	app.NewAccountQuotaHTTP(func(c *gin.Context) adjust_quota.Audit { return accountQuotaAudit{context: c} }).Handle(c, adjust_quota.Request{Id: req.Id, Action: req.Action, Value: req.Value, Mode: req.Mode})
}

// accountQuotaAudit bridges the existing audit implementation with live request metadata.
type accountQuotaAudit struct{ context *gin.Context }

func (a accountQuotaAudit) RecordRecipientAdjustment(adjustment *accounts.UserQuotaAdjustment, action string, params map[string]any) {
	c := a.context
	operation := model.AuditOperation{Action: action, Params: params}
	model.RecordLogWithAdminInfo(adjustment.UserID, model.LogTypeTopup, auditContentEN(action, params), auditOperatorInfo(c), &operation, c)
}
func (a accountQuotaAudit) RecordOperatorAdjustment(action string, params map[string]any, success bool) {
	c := a.context
	content := auditContentEN(action, params)
	if !success {
		content = "Failed user quota adjustment"
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params, auditOperatorInfo(c), &model.AuditRequestInfo{Method: c.Request.Method, Route: c.FullPath(), Status: c.Writer.Status(), Success: success}, c)
	markAuditLogged(c)
}
