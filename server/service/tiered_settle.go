package service

import (
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func BuildTieredTokenParams(usage *dto.Usage, claude bool, used map[string]bool) billingexpr.TokenParams {
	return BillingGateway.TokenParams(usage, claude, used)
}
