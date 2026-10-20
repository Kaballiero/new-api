package model

import (
	"github.com/QuantumNous/new-api/constant"
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/rebuild_catalogue"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
	"time"
)

func pricingBuildChannel(c *Channel) *domain.BuildChannel {
	return (*domain.BuildChannel)(channelrecord.ToDomain((*channelrecord.Channel)(c)))
}
func pricingNativeChannel(c *domain.BuildChannel) *Channel {
	return (*Channel)(store.FromBuildChannel(c))
}
func pricingBuildAbilities(rows []AbilityWithChannel) []domain.BuildAbility {
	out := make([]domain.BuildAbility, len(rows))
	for i, v := range rows {
		out[i] = domain.BuildAbility{Model: v.Model, Group: v.Group, ChannelId: v.ChannelId, ChannelType: v.ChannelType}
	}
	return out
}
func pricingBuildChannels(rows map[int]*Channel) map[int]*domain.BuildChannel {
	out := make(map[int]*domain.BuildChannel, len(rows))
	for id, v := range rows {
		out[id] = pricingBuildChannel(v)
	}
	return out
}
func newPricingCatalogueStore() store.Catalogue {
	return store.NewCatalogue(store.CatalogueEnvironment{
		DB: func() *gorm.DB { return DB }, Abilities: func() ([]domain.BuildAbility, error) {
			rows, err := GetAllEnableAbilityWithChannels()
			return pricingBuildAbilities(rows), err
		},
		Fingerprint: func(m []catalogdomain.Model, v []catalogdomain.Vendor, c []*domain.BuildChannel, a []domain.BuildAbility) (string, error) {
			var mm []Model
			for _, row := range m {
				mm = append(mm, Model(*catalogrecord.FromModel(&row)))
			}
			var vv []Vendor
			for _, row := range v {
				vv = append(vv, Vendor(*catalogrecord.FromVendor(&row)))
			}
			var cc []*Channel
			for _, row := range c {
				cc = append(cc, pricingNativeChannel(row))
			}
			var aa []AbilityWithChannel
			for _, row := range a {
				aa = append(aa, AbilityWithChannel{Ability: Ability{Model: row.Model, Group: row.Group, ChannelId: row.ChannelId}, ChannelType: row.ChannelType})
			}
			return pricingPersistedInputFingerprint(mm, vv, cc, aa)
		},
		ConfiguredCatalog: loadConfiguredCatalog, CatalogEntry: func(c *domain.BuildChannel, name string, endpoint constant.EndpointType) (domain.PricingCatalogEntry, bool) {
			v, ok := GetPricingCatalogEntry(pricingNativeChannel(c), name, endpoint)
			return domain.PricingCatalogEntry(v), ok
		},
	})
}
func newPricingOperations() *domain.Operations {
	return domain.NewOperations(domain.OperationRuntime{AudioRatio: ratio_setting.GetAudioRatio, AudioCompletionRatio: ratio_setting.GetAudioCompletionRatio, BatchExpression: billing_setting.GetBatchBillingExpr, CatalogEntry: func(c *domain.BuildChannel, name string, endpoint constant.EndpointType) (domain.PricingCatalogEntry, bool) {
		v, ok := GetPricingCatalogEntry(pricingNativeChannel(c), name, endpoint)
		return domain.PricingCatalogEntry(v), ok
	}})
}

type pricingBuildState struct{}

func (pricingBuildState) Generation() uint64 { return pricingInputsGeneration.Load() }
func (pricingBuildState) Attempt(g, p uint64) {
	pricingAttemptGeneration.Store(g)
	pricingAttemptPluginGeneration.Store(p)
}
func (pricingBuildState) RetryAfter(value int64) { pricingRetryAfter.Store(value) }
func (pricingBuildState) LockPublication() func() {
	pricingPublicationLock.Lock()
	return pricingPublicationLock.Unlock
}
func (pricingBuildState) Publish(value rebuild_catalogue.Publication) {
	pricingMap, vendorsList = value.Pricing, value.Vendors
	modelSupportEndpointTypes, supportedEndpointMap = value.EndpointTypes, value.Endpoints
	pricingCatalogueRevision, pricingCatalogueRevisionError = value.Revision, nil

	modelEnableGroupsLock.Lock()
	modelEnableGroups = make(map[string][]string)
	modelQuotaTypeMap = make(map[string]int)
	for _, p := range value.Pricing {
		modelEnableGroups[p.ModelName] = p.EnableGroup
		modelQuotaTypeMap[p.ModelName] = p.QuotaType
	}
	modelEnableGroupsLock.Unlock()
	lastGetPricingTime = time.Now()
	pricingRevision.Add(1)
	pricingBuiltGeneration = value.Generation
	pricingBuiltPluginGeneration = value.PluginGeneration
	pricingRetryAfter.Store(0)
	pricingObservedSource.Store(&value.SourceFingerprint)
	pricingObservedChannels.Store(&value.ChannelFingerprint)
	publishedPricing.Store(&publishedPricingCatalogue{pricing: value.Pricing, vendors: value.Vendors, revision: value.Revision, endpoints: value.Endpoints, builtAt: lastGetPricingTime, generation: value.Generation, pluginGeneration: value.PluginGeneration})
}
