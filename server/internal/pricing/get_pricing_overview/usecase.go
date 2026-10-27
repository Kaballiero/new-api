package get_pricing_overview

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
type Groups interface{ ContainsGroup(string) bool }
type Usecase struct {
	deps       domain.Capabilities
	reader     Reader
	projection *domain.Projection
	groups     Groups
}

func NewUsecase(deps domain.Capabilities, reader Reader, projection *domain.Projection, groups Groups) Usecase {
	return Usecase{deps: deps, reader: reader, projection: projection, groups: groups}
}
func (u Usecase) Execute(input Input) (Output, error) {
	group := input.Group
	if group == "" {
		group = "default"
	}
	if group == "auto" || !u.groups.ContainsGroup(group) {
		return Output{}, domain.ErrInvalidUserGroup
	}
	usingGroup := input.UsingGroup
	if usingGroup == "" {
		usingGroup = group
	}
	ctx, err := u.consumerContext(group, usingGroup)
	if err != nil {
		if err == domain.ErrPricingUsingGroup {
			return Output{}, domain.InvalidQuery(err)
		}
		return Output{}, err
	}
	return u.projection.BuildEffectivePricingOverview(ctx, input.Query), nil
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
