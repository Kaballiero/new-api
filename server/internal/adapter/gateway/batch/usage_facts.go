package batch

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/relay/billing"
	"github.com/QuantumNous/new-api/service"
)

func (u Usage) Facts(job *domain.Job, result domain.Result) (domain.UsageFacts, error) {
	usage, err := u.Decode(job, result)
	if err != nil {
		return domain.UsageFacts{}, err
	}
	raw, err := common.Marshal(usage)
	return domain.UsageFacts{Usage: billing.CostUsage(service.EffectiveBillingUsage(usage)), Wire: raw}, err
}

func (Gateway) UsageFacts(job *domain.Job, result domain.Result) (domain.UsageFacts, error) {
	return (Usage{}).Facts(job, result)
}
