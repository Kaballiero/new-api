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
	reader     Reader
	projection *domain.Projection
	users      Users
}

func NewUsecase(reader Reader, projection *domain.Projection, users Users) Usecase {
	return Usecase{reader: reader, projection: projection, users: users}
}
func (u Usecase) Execute(input Input) (Output, error) {
	group, err := u.users.UserGroup(input.UserID)
	if err != nil {
		return Output{}, &domain.ReadFailure{Kind: domain.UserUnavailable, Message: "failed to resolve user pricing group"}
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
		return Output{}, &domain.ReadFailure{Kind: domain.NotFound, Message: "model pricing is unavailable"}
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
	ctx.Metadata.CatalogRevision, err = domain.ConsumerPricingRevision(source, ctx)
	return ctx, err
}
