package pricingstate

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	store "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/rebuild_catalogue"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

func PricingBuildChannel(c *legacy.Channel) *domain.BuildChannel {
	return (*domain.BuildChannel)(channelrecord.ToDomain((*channelrecord.Channel)(c)))
}

func pricingNativeChannel(c *domain.BuildChannel) *legacy.Channel {
	return (*legacy.Channel)(store.FromBuildChannel(c))
}

func PricingBuildAbilities(rows []channelrecord.AbilityWithChannel) []domain.BuildAbility {
	out := make([]domain.BuildAbility, len(rows))
	for i, v := range rows {
		out[i] = domain.BuildAbility{Model: v.Model, Group: v.Group, ChannelId: v.ChannelId, ChannelType: v.ChannelType}
	}
	return out
}

func pricingBuildChannels(rows map[int]*legacy.Channel) map[int]*domain.BuildChannel {
	out := make(map[int]*domain.BuildChannel, len(rows))
	for id, v := range rows {
		out[id] = PricingBuildChannel(v)
	}
	return out
}

func NewPricingCatalogueStore(configured func(context.Context) (map[string]*domain.SourceDocument, map[string]string, bool, error)) store.Catalogue {
	return store.NewCatalogue(store.CatalogueEnvironment{

		DB: func() *gorm.DB { return legacy.DB },
		Abilities: func() ([]domain.BuildAbility, error) {
			rows, err := channelrecord.NewStore(channelrecord.Environment{DB: func() *gorm.DB { return legacy.DB }}).GetAllEnableAbilityWithChannels()
			return PricingBuildAbilities(rows), err
		},

		Fingerprint: func(m []catalogdomain.Model, v []catalogdomain.Vendor, c []*domain.BuildChannel, a []domain.BuildAbility) (string, error) {
			var mm []legacy.Model
			for _, row := range m {
				mm = append(mm, legacy.Model(*catalogrecord.FromModel(&row)))
			}
			var vv []legacy.Vendor
			for _, row := range v {
				vv = append(vv, legacy.Vendor(*catalogrecord.FromVendor(&row)))
			}
			var cc []*legacy.Channel
			for _, row := range c {
				cc = append(cc, pricingNativeChannel(row))
			}
			var aa []channelrecord.AbilityWithChannel
			for _, row := range a {
				aa = append(
					aa,
					channelrecord.AbilityWithChannel{
						Ability: channelrecord.Ability{
							Model:     row.Model,
							Group:     row.Group,
							ChannelId: row.ChannelId,
						},
						ChannelType: row.ChannelType,
					},
				)
			}
			return PricingPersistedInputFingerprint(mm, vv, cc, aa)
		},

		ConfiguredCatalog: configured,
		CatalogEntry: func(
			c *domain.BuildChannel,
			name string,
			endpoint constant.EndpointType,
		) (domain.PricingCatalogEntry, bool) {
			v, ok := GetPricingCatalogEntry(pricingNativeChannel(c), name, endpoint)
			return domain.PricingCatalogEntry(v), ok
		},
	})
}

func NewPricingOperations() *domain.Operations {
	return domain.NewOperations(domain.OperationRuntime{
		Capabilities:         pricingCapabilities(),
		AudioRatio:           ratio_setting.GetAudioRatio,
		AudioCompletionRatio: ratio_setting.GetAudioCompletionRatio,
		BatchExpression:      billing_setting.GetBatchBillingExpr,
		CatalogEntry: func(
			c *domain.BuildChannel,
			name string,
			endpoint constant.EndpointType,
		) (domain.PricingCatalogEntry, bool) {
			v, ok := GetPricingCatalogEntry(pricingNativeChannel(c), name, endpoint)
			return domain.PricingCatalogEntry(v), ok
		},
	})
}

type PricingBuildState struct{}

func (PricingBuildState) Generation() uint64 { return pricingInputsGeneration.Load() }

func (PricingBuildState) Attempt(g, p uint64) {
	pricingAttemptGeneration.Store(g)
	pricingAttemptPluginGeneration.Store(p)
}

func (PricingBuildState) RetryAfter(value int64) { pricingRetryAfter.Store(value) }

func (PricingBuildState) LockPublication() func() {
	pricingPublicationLock.Lock()
	return pricingPublicationLock.Unlock
}

func (PricingBuildState) Publish(value rebuild_catalogue.Publication) {
	pricingMap, vendorsList = value.Pricing, value.Vendors
	nativeEndpoints := make(map[string]common.EndpointInfo, len(value.Endpoints))
	for key, endpoint := range value.Endpoints {
		nativeEndpoints[key] = common.EndpointInfo(endpoint)
	}
	modelSupportEndpointTypes = value.EndpointTypes

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
	PricingObservedChannels.Store(&value.ChannelFingerprint)
	publishedPricing.Store(&publishedPricingCatalogue{
		pricing:          value.Pricing,
		vendors:          value.Vendors,
		revision:         value.Revision,
		endpoints:        nativeEndpoints,
		builtAt:          lastGetPricingTime,
		generation:       value.Generation,
		pluginGeneration: value.PluginGeneration,
	})
}
