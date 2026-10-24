package bind_subscription

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type HTTP struct {
	usecase    Usecase
	compliance func() bool
}

func NewHTTP(usecase Usecase, compliance func() bool) HTTP { return HTTP{usecase, compliance} }

type AdminBindSubscriptionRequest struct {
	UserID int `json:"user_id"`
	PlanID int `json:"plan_id"`
}
type AdminCreateUserSubscriptionRequest struct {
	PlanID int `json:"plan_id"`
}

func (h HTTP) Handle(c *gin.Context) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	var req AdminBindSubscriptionRequest
	if c.ShouldBindJSON(&req) != nil || req.UserID <= 0 || req.PlanID <= 0 {
		transport.InvalidParameters(c)
		return
	}
	h.respond(c, req.UserID, req.PlanID)
}
func (h HTTP) CreateForUser(c *gin.Context) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		transport.WriteError(c, domain.ErrInvalidUserID)
		return
	}
	var req AdminCreateUserSubscriptionRequest
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 {
		transport.InvalidParameters(c)
		return
	}
	h.respond(c, id, req.PlanID)
}
func (h HTTP) respond(c *gin.Context, userID, planID int) {
	result, err := h.usecase.Execute(Input{UserID: userID, PlanID: planID})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if result.UpgradeGroup != "" {
		common.ApiSuccess(c, gin.H{"message": i18n.T(c, i18n.MsgSubscriptionsGroupUpgrade, map[string]any{"Group": result.UpgradeGroup})})
		return
	}
	common.ApiSuccess(c, nil)
}
