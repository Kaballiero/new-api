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
	Billable([]domain.Pricing, string, string) map[string]bool
}
type Groups interface{ ContainsGroup(string) bool }
type Cache interface {
	Get(string) (domain.ConsumerPricingResponse, bool)
	Put(string, domain.ConsumerPricingResponse)
}

// EffectivePricingLists keeps the latest projected list of every tariff.
// A list built from other inputs is returned while build replaces it in the
// background; the request runs build only when the tariff has no list yet.
type EffectivePricingLists interface {
	Load(userGroup, usingGroup string, inputs domain.PricingProjectionInputs, build func() domain.EffectivePricingList) domain.EffectivePricingList
}
type Usecase struct {
	deps       domain.Capabilities
	reader     Reader
	projection *domain.Projection
	cache      Cache
	lists      EffectivePricingLists
	groups     Groups
}

func NewUsecase(deps domain.Capabilities, reader Reader, projection *domain.Projection, cache Cache, lists EffectivePricingLists, groups Groups) Usecase {
	return Usecase{deps: deps, reader: reader, projection: projection, cache: cache, lists: lists, groups: groups}
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
			return Output{}, domain.InvalidQuery(err)
		}
		return Output{}, err
	}

	if input.Page {
		ctx.Billable = u.reader.Billable(ctx.Pricing, ctx.UsingGroup, ctx.Metadata.CatalogRevision)
		inputs := domain.PricingProjectionInputs{CatalogRevision: ctx.Metadata.CatalogRevision, Billable: ctx.Billable}
		effectivePricingList := u.lists.Load(group, usingGroup, inputs, func() domain.EffectivePricingList {
			return u.projection.BuildEffectivePricingList(ctx, true, ctx.Project)
		})
		// A list kept while its rebuild is pending carries its own revision.
		if err := domain.CheckRevision(input.Query.ExpectedRevision, effectivePricingList.Metadata.CatalogRevision); err != nil {
			return Output{}, err
		}
		page := effectivePricingList.Page(input.Query)
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
