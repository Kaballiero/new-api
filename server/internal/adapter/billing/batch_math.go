package billing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type BatchMath struct {
	Expression
	CostMath
}

func (BatchMath) Cost(raw []byte) (float64, error) { return common.ValidateOpenRouterCost(raw) }
func (BatchMath) Raw(expr string, p domain.TokenParams, r domain.RequestInput) (float64, error) {
	v, _, e := billingexpr.RunExprWithRequest(expr, billingexpr.TokenParams(p), billingexpr.RequestInput(r))
	return v, e
}

func (BatchMath) Hash(expr string) string { return billingexpr.ExprHashString(expr) }
