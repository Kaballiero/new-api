package get_public_models

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
	Billable([]domain.Pricing, string, string) map[string]bool
}

// PublicPricingModelsLists keeps the latest projected list of every tariff.
// A list built from other inputs is returned while build replaces it in the
// background; the request runs build only when the tariff has no list yet.
type PublicPricingModelsLists interface {
	Load(userGroup, usingGroup string, inputs domain.PricingProjectionInputs, build func() domain.PublicPricingModelsList) domain.PublicPricingModelsList
}
type Usecase struct {
	deps       domain.Capabilities
	reader     Reader
	projection *domain.Projection
	lists      PublicPricingModelsLists
}

func NewUsecase(deps domain.Capabilities, reader Reader, projection *domain.Projection, lists PublicPricingModelsLists) Usecase {
	return Usecase{deps: deps, reader: reader, projection: projection, lists: lists}
}
func (u Usecase) Execute(input Input) (Output, error) {
	ctx, err := u.consumerContext("default", "default")
	if err != nil {
		return Output{}, domain.ErrPublicPricingUnavailable
	}
	ctx.Billable = u.reader.Billable(ctx.Pricing, "default", ctx.Metadata.CatalogRevision)
	inputs := domain.PricingProjectionInputs{CatalogRevision: ctx.Metadata.CatalogRevision, Billable: ctx.Billable}
	publicModels := u.lists.Load("default", "default", inputs, func() domain.PublicPricingModelsList {
		return u.projection.BuildPublicPricingModelsList(ctx)
	})
	// A catalogue kept while its rebuild is pending carries its own revision.
	if err = domain.CheckRevision(input.ExpectedRevision, publicModels.CatalogRevision); err != nil {
		return Output{}, err
	}
	return publicModels.Page(input), nil
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
