package confirm_batch_purchase

import (
	"context"
	"strings"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/shopspring/decimal"
)

type Store interface {
	ConfirmBatchPurchaseCost(string, string, string) error
}
type Input struct {
	PublicID string `json:"-"`
	CostUSD  string `json:"cost_usd"`
	Evidence string `json:"evidence"`
}
type Usecase struct{ store Store }

func NewUsecase(s Store) Usecase { return Usecase{s} }
func (u Usecase) Execute(ctx context.Context, in Input) error {
	if len(in.CostUSD) > 128 || strings.TrimSpace(in.Evidence) == "" || len(in.Evidence) > 4096 {
		return &domain.RuleError{Kind: domain.InvalidPurchaseEvidence}
	}
	cost, err := decimal.NewFromString(in.CostUSD)
	if err != nil || cost.IsNegative() || cost.Exponent() < -12 || cost.Exponent() > 12 || cost.Coefficient().BitLen() > 128 {
		return &domain.RuleError{Kind: domain.InvalidPurchaseCost}
	}
	if err := u.store.ConfirmBatchPurchaseCost(in.PublicID, cost.String(), in.Evidence); err != nil {
		return &domain.RuleError{Kind: domain.PurchaseReconciliationConflict}
	}
	return nil
}
