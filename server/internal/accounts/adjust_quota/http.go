package adjust_quota

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/gin-gonic/gin"
)

type Audit interface {
	RecordRecipientAdjustment(*UserQuotaAdjustment, string, map[string]any)
	RecordOperatorAdjustment(string, map[string]any, bool)
}
type HTTP struct {
	scenario Usecase
	newAudit func(*gin.Context) Audit
}

func NewHTTP(scenario Usecase, newAudit func(*gin.Context) Audit) *HTTP {
	return &HTTP{scenario: scenario, newAudit: newAudit}
}
func (h *HTTP) Handle(c *gin.Context, req Request) {
	action := "generic"
	params := map[string]any{
		"target_user_id":  req.Id,
		"mode":            req.Mode,
		"requested_quota": req.Value,
	}
	switch req.Mode {
	case "add":
		action = "user.quota_add"
	case "subtract":
		action = "user.quota_subtract"
	case "override":
		action = "user.quota_override"
	default:
		params["action"] = "add_quota"
		params["method"] = c.Request.Method
		params["route"] = c.FullPath()
	}
	success := false
	requestAudit := h.newAudit(c)
	defer func() { requestAudit.RecordOperatorAdjustment(action, params, success) }()

	adjustment, err := h.scenario.Execute(req.Id, c.GetInt("role"), req.Mode, req.Value)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrQuotaChangeZero), errors.Is(err, domain.ErrQuotaAdjustmentInvalid):
			params["failure_reason"] = "invalid_parameters"
		case errors.Is(err, domain.ErrManageForbidden):
			params["failure_reason"] = "permission_denied"
		case errors.Is(err, domain.ErrAccountNotFound):
			params["failure_reason"] = "target_not_found"
		case errors.Is(err, domain.ErrQuotaLimitExceeded):
			params["failure_reason"] = "quota_limit_exceeded"
		default:
			params["failure_reason"] = "database_error"
		}
		transport.WriteError(c, err)
		return
	}

	params["target_username"] = adjustment.Username
	params["from"] = adjustment.Before
	params["to"] = adjustment.After
	if req.Mode != "override" {
		params["quota"] = req.Value
	}
	success = true
	requestAudit.RecordRecipientAdjustment(adjustment, action, params)
	common.ApiSuccess(c, Response{Action: req.Action, UserId: adjustment.UserID, Role: adjustment.Role, Status: adjustment.Status, Quota: adjustment.After})
}
