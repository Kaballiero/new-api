package get_model_pricing

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Reader interface {
	Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error)
	FX() (domain.BillingFXBasis, error)
	AutoGroups(string) []string
	UsableGroups(string) map[string]string
	GroupRatio(string, string) float64
}
type Users interface{ UserGroup(int) (string, error) }
type Usecase struct {
	deps       domain.Capabilities
	reader     Reader
	projection *domain.Projection
	users      Users
}

func NewUsecase(deps domain.Capabilities, reader Reader, projection *domain.Projection, users Users) Usecase {
	return Usecase{deps: deps, reader: reader, projection: projection, users: users}
}
func (u Usecase) Execute(input Input) (Output, error) {
	group, err := u.users.UserGroup(input.UserID)
	if err != nil {
		return Output{}, domain.ErrUserGroupUnavailable
	}
	ctx, err := u.consumerContext(group, "")
	if err != nil {
		return Output{}, err
	}
	if err = domain.CheckRevision(input.ExpectedRevision, ctx.Metadata.CatalogRevision); err != nil {
		return Output{}, err
	}
	ctx.UserID = input.UserID
	detail, found := u.projection.BuildEffectivePricingModelDetail(ctx, input.Name)
	if !found {
		return Output{}, domain.ErrModelPricingUnavailable
	}
	return detail, nil
}
func (u Usecase) consumerContext(userGroup, usingGroup string) (domain.EffectivePricingPageContext, error) {
	ctx := domain.EffectivePricingPageContext{Policy: u.projection, Metadata: domain.ConsumerPricingResponse{SchemaVersion: "v2", UserGroup: userGroup, Currency: "RUB", AutoGroups: u.reader.AutoGroups(userGroup), Data: []domain.ConsumerPricingModel{}}, UsableGroups: u.reader.UsableGroups(userGroup), GroupRatios: make(map[string]float64), UsingGroup: usingGroup}
	if usingGroup != "" {
		if _, allowed := ctx.UsableGroups[usingGroup]; !allowed {
			return ctx, domain.ErrPricingUsingGroup
		}
	}
	basis, err := u.reader.FX()
	if err != nil {
		return ctx, err
	}
	ctx.FxFactor = basis.Factor
	source := ""
	ctx.Pricing, ctx.Vendors, source, err = u.reader.Snapshot()
	if err != nil {
		return ctx, fmt.Errorf("failed to resolve pricing catalog revision")
	}
	for group := range ctx.UsableGroups {
		ctx.GroupRatios[group] = u.reader.GroupRatio(userGroup, group)
	}
	if usingGroup != "" {
		ctx.UsableGroups = map[string]string{usingGroup: ctx.UsableGroups[usingGroup]}
	}
	ctx.Metadata.CatalogRevision, err = domain.ConsumerPricingRevision(u.deps, source, ctx)
	return ctx, err
}
