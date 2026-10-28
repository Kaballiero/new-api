package subscriptions

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	auditapp "github.com/QuantumNous/new-api/internal/app/audit"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	audittransport "github.com/QuantumNous/new-api/internal/audit/transport"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"

	"github.com/gin-gonic/gin"
)

type ResetAudit struct{ audittransport.Recording }

func NewResetAudit() ResetAudit                                  { return ResetAudit{auditapp.NewRecording()} }
func (ResetAudit) Operator(c *gin.Context) *auditdomain.Operator { return audittransport.Operator(c) }
func (a ResetAudit) RecordUserLogs(c *gin.Context, result *domain.ResetResult, operator *auditdomain.Operator) {
	if result == nil || result.ResetCount == 0 {
		return
	}
	for _, id := range result.AffectedUserIds {
		a.Subscription(
			c,
			id,
			i18n.Translate(
				receiptLanguage(id),
				i18n.MsgSubscriptionsResetAudit,
				map[string]any{
					"Plan": result.PlanTitle,
					"ID":   result.PlanId,
				},
			),
			operator,
		)
	}
}
func (ResetAudit) PlanReset(result domain.ResetResult) {
	common.SysLog(fmt.Sprintf(
		"admin reset subscription plan %d quota: reset_count=%d user_count=%d advance_reset_time=%t",
		result.PlanId,
		result.ResetCount,
		result.UserCount,
		result.AdvanceResetTime,
	))
}
