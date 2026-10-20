package get_group_pricing

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
type Cache interface {
	Get(string) (domain.ConsumerPricingResponse, bool)
	Put(string, domain.ConsumerPricingResponse)
}
type Usecase struct {
	reader     Reader
	projection *domain.Projection
	cache      Cache
	groups     Groups
}

func NewUsecase(reader Reader, projection *domain.Projection, cache Cache, groups Groups) Usecase {
	return Usecase{reader: reader, projection: projection, cache: cache, groups: groups}
}
func (u Usecase) Execute(input Input) (Output, error) {
	group := input.Group
	if group == "" {
		group = "default"
	}
	if group == "auto" || !u.groups.ContainsGroup(group) {
		return Output{}, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: "invalid user pricing group"}
	}
	usingGroup := input.UsingGroup
	if input.Page {
		if usingGroup == "" {
			usingGroup = group
		}
	} else {
		usingGroup = ""
	}
	ctx, err := u.consumerContext(group, usingGroup)
	if err != nil {
		if input.Page && err == domain.ErrPricingUsingGroup {
			return Output{}, &domain.ReadFailure{Kind: domain.InvalidQuery, Message: err.Error()}
		}
		return Output{}, err
	}

	if input.Page {
		if err := domain.CheckRevision(input.Query.ExpectedRevision, ctx.Metadata.CatalogRevision); err != nil {
			return Output{}, err
		}
		page := u.projection.BuildEffectivePricingPage(ctx, input.Query, ctx.Project)
		return Output{Page: &page}, nil
	}
	cacheable := ctx.UserID == 0
	cached, known := u.cache.Get(group)
	if cacheable && known && cached.CatalogRevision == ctx.Metadata.CatalogRevision {
		return Output{Full: &cached}, nil
	}
	response := ctx.Metadata
	for _, item := range domain.ConsumerPricingCatalogue(ctx.Pricing, ctx.UsableGroups) {
		response.Data = append(response.Data, ctx.Project(item))
	}
	if cacheable {
		u.cache.Put(group, response)
	}
	return Output{Full: &response}, nil
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
