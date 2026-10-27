package pricingstate

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/constant"
	pricingrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/record"

	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"

	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"

	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var TestLastGetPricingTime = &lastGetPricingTime
var TestPricingCatalogState = &pricingCatalogState
var TestPublishedPricing = &publishedPricing
var TestPricingInputsGeneration = &pricingInputsGeneration
var TestPricingRetryAfter = &pricingRetryAfter
var TestPricingRefreshRunning = &pricingRefreshRunning
var TestUpdatePricingLock = &updatePricingLock
var TestModelSupportEndpointsLock = &modelSupportEndpointsLock
var TestLegacyVideoPriceBasis = legacyVideoPriceBasis
var TestInitDefaultVendorMapping = initDefaultVendorMapping
var TestCatalogConsumerProfile = catalogConsumerProfile
var TestCatalogConsumerOperation = catalogConsumerOperation
var TestConfiguredPricingOperation = configuredPricingOperation
var TestConfiguredOperationOffers = configuredOperationOffers
var TestProjectPricingOperations = projectPricingOperations
var TestPairedPricingBatchBase = pairedPricingBatchBase
var TestConfiguredVeoUsageOffers = configuredVeoUsageOffers
var TestImagePricingWithTokenInputs = imagePricingWithTokenInputs
var TestPricingCatalogueFingerprint = pricingCatalogueFingerprint
var TestPricingCatalogEntryKey = pricingCatalogEntryKey
var TestPricingCatalogFingerprint = pricingCatalogFingerprint
var TestPricingCapabilities = pricingCapabilities

const TestPricingCatalogMaxBytes = 8 << 20
const TestPricingBuildRetryDelay = pricingBuildRetryDelay

type TestPricingOperation = pricingOperation

func NewOperationForTest(operation, main, basis string) pricingOperation {
	return pricingOperation{operation, main, basis}
}
func OperationNameForTest(p pricingOperation) string                 { return p.operation }
func OperationBasisForTest(p pricingOperation) string                { return p.basis }
func BuiltAtForTest(p *publishedPricingCatalogue) *time.Time         { return &p.builtAt }
func PublicationRevisionForTest(p *publishedPricingCatalogue) string { return p.revision }

func legacyVideoPriceBasis(
	generation *jsplugin.RoutingGeneration,
	channel *legacy.Channel,
	modelName, taskPluginKey string,
) (map[string]jsplugin.LegacyPriceBasis, bool, bool) {
	basis, known, only := pricingdomain.LegacyVideoPriceBasis(
		pricingCapabilities(),
		pricingadapter.NewGeneration(generation),
		PricingBuildChannel(channel),
		modelName,
		taskPluginKey,
	)
	var result map[string]jsplugin.LegacyPriceBasis
	if basis != nil {
		result = make(map[string]jsplugin.LegacyPriceBasis, len(basis))
		for key, value := range basis {
			result[key] = jsplugin.LegacyPriceBasis(value)
		}
	}
	return result, known, only
}

func initDefaultVendorMapping(metadata map[string]*legacy.Model, vendors map[int]*legacy.Vendor, abilities []channelrecord.AbilityWithChannel) {
	m := make(map[string]*pricingdomain.BuildModel, len(metadata))
	v := make(map[int]*pricingdomain.BuildVendor, len(vendors))
	for name, row := range metadata {
		m[name] = catalogrecord.ToModel((*catalogrecord.Model)(row))
	}
	for id, row := range vendors {
		v[id] = catalogrecord.ToVendor((*catalogrecord.Vendor)(row))
	}
	pricingdomain.InitDefaultVendorMapping(m, v, PricingBuildAbilities(abilities))
	for name, row := range m {
		if metadata[name] == nil {
			metadata[name] = (*legacy.Model)(catalogrecord.FromModel(row))
		}
	}
	for id, row := range v {
		if vendors[id] == nil {
			vendors[id] = (*legacy.Vendor)(catalogrecord.FromVendor(row))
		}
	}
}

func catalogConsumerProfile(document *catalogsync.Document) (pricingOperation, string) {
	v, modality := pricingdomain.CatalogConsumerProfile(pricingCatalogDocument(document))
	return pricingOperation{v.Operation, v.Main, v.Basis}, modality
}

func catalogConsumerOperation(document *catalogsync.Document, upstream string, endpoint constant.EndpointType) pricingOperation {
	v := pricingdomain.CatalogConsumerOperation(pricingCatalogDocument(document), upstream, endpoint)
	return pricingOperation{v.Operation, v.Main, v.Basis}
}

func configuredPricingOperation(channel *legacy.Channel, public, upstream string, endpoint constant.EndpointType) pricingOperation {
	v := pricingdomain.ConfiguredPricingOperation(pricingCapabilities(), PricingBuildChannel(channel), public, upstream, endpoint)
	return pricingOperation{v.Operation, v.Main, v.Basis}
}

func configuredOperationOffers(pricing pricingdomain.Pricing, profile pricingOperation, mode, expression string, public ...bool) []pricingdomain.CatalogOffer {
	return NewPricingOperations().ConfiguredOperationOffers(pricing, pricingdomain.PricingOperation{
		Operation: profile.operation,
		Main:      profile.main,
		Basis:     profile.basis,
	}, mode, expression, public...)
}

func projectPricingOperations(
	pricing *pricingdomain.Pricing,
	abilities []channelrecord.AbilityWithChannel,
	channels map[int]*legacy.Channel,
	generation *jsplugin.RoutingGeneration,
	advanced map[int]*dto.AdvancedCustomConfig,
	keys map[int]string,
) {
	NewPricingOperations().ProjectPricingOperations(
		pricing,
		PricingBuildAbilities(abilities),
		pricingBuildChannels(channels),
		pricingadapter.NewGeneration(generation),
		pricingadapter.AdvancedConfigurations(advanced),
		keys,
	)
}

func pairedPricingBatchBase(
	ability channelrecord.AbilityWithChannel,
	upstream string,
	abilities []channelrecord.AbilityWithChannel,
	channels map[int]*legacy.Channel,
) (string, bool) {
	return NewPricingOperations().PairedPricingBatchBase(
		PricingBuildAbilities([]channelrecord.AbilityWithChannel{ability})[0],
		upstream,
		PricingBuildAbilities(abilities),
		pricingBuildChannels(channels),
	)
}

func ConsumerPricingCatalogue(pricing []pricingdomain.Pricing, groups map[string]string) []pricingdomain.Pricing {
	return pricingdomain.ConsumerPricingCatalogue(pricing, groups)
}

func imagePricingWithTokenInputs(offers []pricingdomain.CatalogOffer) []pricingdomain.CatalogOffer {
	return pricingdomain.ImagePricingWithTokenInputs(offers)
}

func configuredVeoUsageOffers(pricing pricingdomain.Pricing, channel *legacy.Channel, upstream string, generation *jsplugin.RoutingGeneration) []pricingdomain.CatalogOffer {
	return pricingdomain.ConfiguredVeoUsageOffers(
		pricingCapabilities(),
		pricing,
		PricingBuildChannel(channel),
		upstream,
		pricingadapter.NewGeneration(generation),
	)
}

type pricingOperation struct{ operation, main, basis string }

func pricingCatalogueFingerprint(pricing []pricingdomain.Pricing, vendors []pricingdomain.PricingVendor) (string, error) {
	return pricingdomain.PricingCatalogueFingerprint(pricingCapabilities(), pricing, vendors)
}

func GetPricingRevision() uint64 {
	return pricingRevision.Load()
}

func GetCatalogDocuments(ctx context.Context, sourceID string, hashes []string) (map[string]pricingrecord.CatalogDocument, error) {
	rows, err := NewImmutableCatalogPersistence().Documents(ctx, sourceID, hashes)
	if err != nil {
		return nil, err
	}
	out := make(map[string]pricingrecord.CatalogDocument, len(rows))
	for key, v := range rows {
		out[key] = pricingrecord.CatalogDocument(v)
	}
	return out, nil
}

func ReadCatalogSelection(ctx context.Context, sourceID, revision string) (pricingdomain.CatalogSelection, error) {
	return NewImmutableCatalogPersistence().ReadSelection(ctx, sourceID, revision)
}

func PricingCatalogSourceFingerprint(channel *legacy.Channel, upstream string, endpoint constant.EndpointType) string {
	return pricingdomain.PricingCatalogSourceFingerprint(pricingCapabilities(), PricingBuildChannel(channel), upstream, endpoint)
}

func SelectCatalogOffer(offers []pricingdomain.CatalogOffer) *pricingdomain.CatalogOffer {
	return pricingdomain.SelectCatalogOffer(offers)
}
