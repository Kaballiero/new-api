package prepare_tiered_cost

import "github.com/QuantumNous/new-api/internal/billing/domain"

type Usecase struct{ math domain.QuotaMath }

func NewUsecase(math domain.QuotaMath) Usecase { return Usecase{math: math} }

type Input struct {
	Snapshot                             *domain.TieredSnapshot
	GroupRatio, PureGroupRatio, FXFactor float64
}

func (u Usecase) Execute(in Input) (*domain.TieredSnapshot, error) {
	snap := in.Snapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return nil, nil
	}
	group := in.GroupRatio
	if in.FXFactor != 0 {
		var err error
		group, err = domain.ApplyBillingFX(in.PureGroupRatio, in.FXFactor)
		if err != nil {
			return nil, err
		}
	}
	if snap.GroupRatio == group {
		return snap, nil
	}
	quota, err := u.math.RoundStrict(snap.EstimatedQuotaBeforeGroup * group)
	if err != nil {
		return nil, err
	}
	result := *snap
	result.GroupRatio = group
	result.EstimatedQuotaAfterGroup = quota
	return &result, nil
}
