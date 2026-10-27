package get_personal_pricing

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
type Cache interface {
	Get(string) (domain.ConsumerPricingResponse, bool)
	Put(string, domain.ConsumerPricingResponse)
}
type Usecase struct {
	deps       domain.Capabilities
	reader     Reader
	projection *domain.Projection
	cache      Cache
	users      Users
}

func NewUsecase(deps domain.Capabilities, reader Reader, projection *domain.Projection, cache Cache, users Users) Usecase {
	return Usecase{deps: deps, reader: reader, projection: projection, cache: cache, users: users}
}
func (u Usecase) Execute(input Input) (Output, error) {
	group := input.Group
	var err error
	group, err = u.users.UserGroup(input.UserID)
	if err != nil {
		return Output{}, domain.ErrUserGroupUnavailable
	}
	usingGroup := input.UsingGroup
	if input.Page {
	} else {
		usingGroup = ""
	}
	ctx, err := u.consumerContext(group, usingGroup)
	if err != nil {
		if input.Page && err == domain.ErrPricingUsingGroup {
			return Output{}, domain.InvalidQuery(err)
		}
		return Output{}, err
	}
	ctx.UserID = input.UserID

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
	ctx.Metadata.CatalogRevision, err = domain.ConsumerPricingRevision(u.deps, source, ctx)
	return ctx, err
}
