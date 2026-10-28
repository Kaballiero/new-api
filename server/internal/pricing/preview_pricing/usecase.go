package preview_pricing

import (
	"slices"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Reader interface {
	GroupRatios() map[string]float64
	UserGroup(int) (string, error)
	FX() (domain.BillingFXBasis, error)
	Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error)
}
type Usecase struct {
	reader     Reader
	projection *domain.Projection
}

func NewUsecase(reader Reader, projection *domain.Projection) Usecase {
	return Usecase{reader: reader, projection: projection}
}
func (u Usecase) Execute(input Input) (Output, error) {
	configured := u.reader.GroupRatios()
	groups := make([]string, 0, len(configured))
	for group := range configured {
		if group != "" && group != "auto" {
			groups = append(groups, group)
		}
	}
	slices.Sort(groups)
	var group string
	if input.Supplied {
		if len(input.UserGroups) != 1 || !slices.Contains(groups, input.UserGroups[0]) {
			return Output{}, domain.ErrInvalidUserGroup
		}
		group = input.UserGroups[0]
	} else {
		var err error
		group, err = u.reader.UserGroup(input.UserID)
		if err != nil {
			return Output{}, domain.ErrUserGroupUnavailable
		}
	}
	basis, err := u.reader.FX()
	if err != nil {
		return Output{}, err
	}
	prices, _, _, err := u.reader.Snapshot()
	if err != nil {
		return Output{}, err
	}
	return Output{EffectivePricingResponse: u.projection.BuildEffectivePricingResponse(group, basis, prices), AvailableUserGroups: groups}, nil
}
