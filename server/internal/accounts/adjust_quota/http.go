package adjust_quota

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/gin-gonic/gin"
)

type Audit interface {
	RecordRecipientAdjustment(*accounts.UserQuotaAdjustment, string, map[string]any)
	RecordOperatorAdjustment(string, map[string]any, bool)
}
type ErrorClassifier interface{ IsRecordNotFound(error) bool }
type ErrorPolicy interface {
	Invalid() error
	Permission() error
	Limit() error
}
type HTTP struct {
	errors     ErrorPolicy
	scenario   Usecase
	newAudit   func(*gin.Context) Audit
	classifier ErrorClassifier
}

func NewHTTP(scenario Usecase, newAudit func(*gin.Context) Audit, classifier ErrorClassifier, errors ErrorPolicy) *HTTP {
	return &HTTP{scenario: scenario, newAudit: newAudit, classifier: classifier, errors: errors}
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
		case errors.Is(err, h.errors.Invalid()):
			params["failure_reason"] = "invalid_parameters"
			if (req.Mode == "add" || req.Mode == "subtract") && req.Value <= 0 {
				common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "quota_change_zero", i18n.MsgUserQuotaChangeZero)
			} else {
				common.ApiErrorI18nStatusCode(c, http.StatusBadRequest, "invalid_params", i18n.MsgInvalidParams)
			}
		case errors.Is(err, h.errors.Permission()):
			params["failure_reason"] = "permission_denied"
			common.ApiErrorI18nStatusCode(c, http.StatusForbidden, "permission_denied", i18n.MsgUserNoPermissionHigherLevel)
		case h.classifier.IsRecordNotFound(err):
			params["failure_reason"] = "target_not_found"
			common.ApiErrorI18nStatusCode(c, http.StatusNotFound, "user_not_found", i18n.MsgUserNotExists)
		case errors.Is(err, h.errors.Limit()):
			params["failure_reason"] = "quota_limit_exceeded"
			common.ApiError(c, err)
		default:
			params["failure_reason"] = "database_error"
			common.ApiErrorStatusCode(c, http.StatusInternalServerError, "internal_error", err)
		}
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
