package bind_subscription

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
	"strconv"
)

type HTTP struct {
	usecase    Usecase
	compliance func() bool
}

func NewHTTP(usecase Usecase, compliance func() bool) HTTP { return HTTP{usecase, compliance} }
func (h HTTP) Handle(c *gin.Context)                       { h.handle(c, false) }
func (h HTTP) CreateForUser(c *gin.Context)                { h.handle(c, true) }
func (h HTTP) handle(c *gin.Context, fromPath bool) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	var req struct {
		UserID int `json:"user_id"`
		PlanID int `json:"plan_id"`
	}
	userID := 0
	if fromPath {
		userID, _ = strconv.Atoi(c.Param("id"))
		if userID <= 0 {
			transport.WriteError(c, domain.ErrInvalidUserID)
			return
		}
	}
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 || (!fromPath && req.UserID <= 0) {
		transport.InvalidParameters(c)
		return
	}
	if !fromPath {
		userID = req.UserID
	}
	result, err := h.usecase.Execute(Input{UserID: userID, PlanID: req.PlanID})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if result.UpgradeGroup != "" {
		common.ApiSuccess(c, gin.H{"message": i18n.Translate(i18n.GetLangFromContext(c), i18n.MsgSubscriptionsGroupUpgrade, map[string]any{"Group": result.UpgradeGroup})})
		return
	}
	common.ApiSuccess(c, nil)
}
