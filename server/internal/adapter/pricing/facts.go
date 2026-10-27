package pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

func ToSourceReference(value catalogsync.Reference) domain.SourceReference {
	var result domain.SourceReference
	result.ID = value.ID
	result.Hash = value.Hash
	return result
}
func nativeSourceReference(value domain.SourceReference) catalogsync.Reference {
	var result catalogsync.Reference
	result.ID = value.ID
	result.Hash = value.Hash
	return result
}
func ToSourceManifest(value catalogsync.Manifest) domain.SourceManifest {
	var result domain.SourceManifest
	result.SchemaVersion = value.SchemaVersion
	result.NormalizationVersion = value.NormalizationVersion
	result.Revision = value.Revision
	result.SourceRevision = value.SourceRevision
	result.FetchedAt = value.FetchedAt
	if value.Models != nil {
		result.Models = make([]domain.SourceReference, len(value.Models))
		for i1 := range value.Models {
			result.Models[i1] = ToSourceReference(value.Models[i1])
		}
	}
	return result
}

func ToSourceCoverage(value catalogsync.Coverage) domain.SourceCoverage {
	var result domain.SourceCoverage
	result.Offers = value.Offers
	result.Normalization = value.Normalization
	result.Reasons = value.Reasons
	result.ExpectedSources = value.ExpectedSources
	result.MissingSources = value.MissingSources
	return result
}
func nativeSourceCoverage(value domain.SourceCoverage) catalogsync.Coverage {
	var result catalogsync.Coverage
	result.Offers = value.Offers
	result.Normalization = value.Normalization
	result.Reasons = value.Reasons
	result.ExpectedSources = value.ExpectedSources
	result.MissingSources = value.MissingSources
	return result
}
func ToSourceProvider(value catalogsync.Provider) domain.SourceProvider {
	var result domain.SourceProvider
	result.ID = value.ID
	result.Name = value.Name
	result.Tag = value.Tag
	return result
}
func nativeSourceProvider(value domain.SourceProvider) catalogsync.Provider {
	var result catalogsync.Provider
	result.ID = value.ID
	result.Name = value.Name
	result.Tag = value.Tag
	return result
}
func ToSourceAlias(value catalogsync.Alias) domain.SourceAlias {
	var result domain.SourceAlias
	result.TargetID = value.TargetID
	return result
}
func nativeSourceAlias(value domain.SourceAlias) catalogsync.Alias {
	var result catalogsync.Alias
	result.TargetID = value.TargetID
	return result
}
func ToSourceAuthor(value catalogsync.Author) domain.SourceAuthor {
	var result domain.SourceAuthor
	result.Namespace = value.Namespace
	return result
}
func nativeSourceAuthor(value domain.SourceAuthor) catalogsync.Author {
	var result catalogsync.Author
	result.Namespace = value.Namespace
	return result
}
func ToSourceModalities(value catalogsync.Modalities) domain.SourceModalities {
	var result domain.SourceModalities
	result.Input = value.Input
	result.Output = value.Output
	return result
}
func nativeSourceModalities(value domain.SourceModalities) catalogsync.Modalities {
	var result catalogsync.Modalities
	result.Input = value.Input
	result.Output = value.Output
	return result
}
func ToSourceDocument(value catalogsync.Document) domain.SourceDocument {
	var result domain.SourceDocument
	result.SchemaVersion = value.SchemaVersion
	result.ID = value.ID
	result.Name = value.Name
	result.Description = value.Description
	if value.Author != nil {
		mapped := ToSourceAuthor(*value.Author)
		result.Author = &mapped
	}
	result.Type = value.Type
	result.Modalities = ToSourceModalities(value.Modalities)
	result.Metadata = value.Metadata
	result.Capabilities = value.Capabilities
	result.Limits = value.Limits
	if value.Alias != nil {
		mapped := ToSourceAlias(*value.Alias)
		result.Alias = &mapped
	}
	if value.Offers != nil {
		result.Offers = make([]domain.SourceOffer, len(value.Offers))
		for i3 := range value.Offers {
			result.Offers[i3] = ToSourceOffer(value.Offers[i3])
		}
	}
	result.Coverage = ToSourceCoverage(value.Coverage)
	result.Limitations = value.Limitations
	result.Provenance = value.Provenance
	return result
}
func nativeSourceDocument(value domain.SourceDocument) catalogsync.Document {
	var result catalogsync.Document
	result.SchemaVersion = value.SchemaVersion
	result.ID = value.ID
	result.Name = value.Name
	result.Description = value.Description
	if value.Author != nil {
		mapped := nativeSourceAuthor(*value.Author)
		result.Author = &mapped
	}
	result.Type = value.Type
	result.Modalities = nativeSourceModalities(value.Modalities)
	result.Metadata = value.Metadata
	result.Capabilities = value.Capabilities
	result.Limits = value.Limits
	if value.Alias != nil {
		mapped := nativeSourceAlias(*value.Alias)
		result.Alias = &mapped
	}
	if value.Offers != nil {
		result.Offers = make([]catalogsync.Offer, len(value.Offers))
		for i4 := range value.Offers {
			result.Offers[i4] = nativeSourceOffer(value.Offers[i4])
		}
	}
	result.Coverage = nativeSourceCoverage(value.Coverage)
	result.Limitations = value.Limitations
	result.Provenance = value.Provenance
	return result
}
func ToSourceEligibility(value catalogsync.Eligibility) domain.SourceEligibility {
	var result domain.SourceEligibility
	result.Eligible = value.Eligible
	result.Reasons = value.Reasons
	return result
}
func nativeSourceEligibility(value domain.SourceEligibility) catalogsync.Eligibility {
	var result catalogsync.Eligibility
	result.Eligible = value.Eligible
	result.Reasons = value.Reasons
	return result
}
func ToSourceOffer(value catalogsync.Offer) domain.SourceOffer {
	var result domain.SourceOffer
	result.ID = value.ID
	result.Scope = value.Scope
	result.Identity = value.Identity
	if value.Provider != nil {
		mapped := ToSourceProvider(*value.Provider)
		result.Provider = &mapped
	}
	result.Operation = value.Operation
	result.Mode = value.Mode
	result.Variant = value.Variant
	result.Capabilities = value.Capabilities
	result.Limits = value.Limits
	if value.Eligibility != nil {
		mapped := ToSourceEligibility(*value.Eligibility)
		result.Eligibility = &mapped
	}
	if value.Components != nil {
		result.Components = make([]domain.SourceRate, len(value.Components))
		for i5 := range value.Components {
			result.Components[i5] = ToSourceRate(value.Components[i5])
		}
	}
	result.Coverage = ToSourceCoverage(value.Coverage)
	result.Limitations = value.Limitations
	result.Facts = value.Facts
	return result
}
func nativeSourceOffer(value domain.SourceOffer) catalogsync.Offer {
	var result catalogsync.Offer
	result.ID = value.ID
	result.Scope = value.Scope
	result.Identity = value.Identity
	if value.Provider != nil {
		mapped := nativeSourceProvider(*value.Provider)
		result.Provider = &mapped
	}
	result.Operation = value.Operation
	result.Mode = value.Mode
	result.Variant = value.Variant
	result.Capabilities = value.Capabilities
	result.Limits = value.Limits
	if value.Eligibility != nil {
		mapped := nativeSourceEligibility(*value.Eligibility)
		result.Eligibility = &mapped
	}
	if value.Components != nil {
		result.Components = make([]catalogsync.Component, len(value.Components))
		for i6 := range value.Components {
			result.Components[i6] = nativeSourceRate(value.Components[i6])
		}
	}
	result.Coverage = nativeSourceCoverage(value.Coverage)
	result.Limitations = value.Limitations
	result.Facts = value.Facts
	return result
}
func ToSourceRate(value catalogsync.Component) domain.SourceRate {
	var result domain.SourceRate
	result.Parameter = value.Parameter
	result.ComponentID = value.ComponentID
	result.Billable = value.Billable
	result.Direction = value.Direction
	result.Modality = value.Modality
	result.AmountUSD = value.AmountUSD
	result.Unit = value.Unit
	result.Quantity = value.Quantity
	result.Variant = value.Variant
	result.Conditions = value.Conditions
	result.Source = value.Source
	result.Provenance = value.Provenance
	result.Normalization = value.Normalization
	return result
}
func nativeSourceRate(value domain.SourceRate) catalogsync.Component {
	var result catalogsync.Component
	result.Parameter = value.Parameter
	result.ComponentID = value.ComponentID
	result.Billable = value.Billable
	result.Direction = value.Direction
	result.Modality = value.Modality
	result.AmountUSD = value.AmountUSD
	result.Unit = value.Unit
	result.Quantity = value.Quantity
	result.Variant = value.Variant
	result.Conditions = value.Conditions
	result.Source = value.Source
	result.Provenance = value.Provenance
	result.Normalization = value.Normalization
	return result
}
func ToTariffPrice(value jsplugin.CatalogPrice) domain.TariffPrice {
	var result domain.TariffPrice
	result.Component = value.Component
	result.Unit = value.Unit
	result.AmountUSD = value.AmountUSD
	return result
}
func nativeTariffPrice(value domain.TariffPrice) jsplugin.CatalogPrice {
	var result jsplugin.CatalogPrice
	result.Component = value.Component
	result.Unit = value.Unit
	result.AmountUSD = value.AmountUSD
	return result
}
func ToTariffUsageCondition(value jsplugin.CatalogUsageCondition) domain.TariffUsageCondition {
	var result domain.TariffUsageCondition
	result.Field = value.Field
	result.Value = value.Value
	result.Negated = value.Negated
	return result
}
func nativeTariffUsageCondition(value domain.TariffUsageCondition) jsplugin.CatalogUsageCondition {
	var result jsplugin.CatalogUsageCondition
	result.Field = value.Field
	result.Value = value.Value
	result.Negated = value.Negated
	return result
}
func ToTariffOffer(value jsplugin.CatalogOffer) domain.TariffOffer {
	var result domain.TariffOffer
	if value.PublicUsageConditions != nil {
		result.PublicUsageConditions = make([]domain.TariffUsageCondition, len(value.PublicUsageConditions))
		for i7 := range value.PublicUsageConditions {
			result.PublicUsageConditions[i7] = ToTariffUsageCondition(value.PublicUsageConditions[i7])
		}
	}
	if value.PublicAlternatives != nil {
		result.PublicAlternatives = make([]domain.TariffOffer, len(value.PublicAlternatives))
		for i8 := range value.PublicAlternatives {
			result.PublicAlternatives[i8] = ToTariffOffer(value.PublicAlternatives[i8])
		}
	}
	if value.PublicConditions != nil {
		result.PublicConditions = make([]domain.ExpressionCondition, len(value.PublicConditions))
		for i9 := range value.PublicConditions {
			result.PublicConditions[i9] = ToExpressionCondition(value.PublicConditions[i9])
		}
	}
	result.PublicConditional = value.PublicConditional
	result.ID = value.ID
	result.Operation = value.Operation
	result.Mode = value.Mode
	result.MainComponent = value.MainComponent
	if value.Prices != nil {
		result.Prices = make([]domain.TariffPrice, len(value.Prices))
		for i10 := range value.Prices {
			result.Prices[i10] = ToTariffPrice(value.Prices[i10])
		}
	}
	return result
}
func nativeTariffOffer(value domain.TariffOffer) jsplugin.CatalogOffer {
	var result jsplugin.CatalogOffer
	if value.PublicUsageConditions != nil {
		result.PublicUsageConditions = make([]jsplugin.CatalogUsageCondition, len(value.PublicUsageConditions))
		for i11 := range value.PublicUsageConditions {
			result.PublicUsageConditions[i11] = nativeTariffUsageCondition(value.PublicUsageConditions[i11])
		}
	}
	if value.PublicAlternatives != nil {
		result.PublicAlternatives = make([]jsplugin.CatalogOffer, len(value.PublicAlternatives))
		for i12 := range value.PublicAlternatives {
			result.PublicAlternatives[i12] = nativeTariffOffer(value.PublicAlternatives[i12])
		}
	}
	if value.PublicConditions != nil {
		result.PublicConditions = make([]billingexpr.TierCondition, len(value.PublicConditions))
		for i13 := range value.PublicConditions {
			result.PublicConditions[i13] = nativeExpressionCondition(value.PublicConditions[i13])
		}
	}
	result.PublicConditional = value.PublicConditional
	result.ID = value.ID
	result.Operation = value.Operation
	result.Mode = value.Mode
	result.MainComponent = value.MainComponent
	if value.Prices != nil {
		result.Prices = make([]jsplugin.CatalogPrice, len(value.Prices))
		for i14 := range value.Prices {
			result.Prices[i14] = nativeTariffPrice(value.Prices[i14])
		}
	}
	return result
}
func ToTariffCatalog(value jsplugin.PriceCatalog) domain.TariffCatalog {
	var result domain.TariffCatalog
	result.FetchedAt = value.FetchedAt
	if value.Models != nil {
		result.Models = make(map[string][]domain.TariffOffer, len(value.Models))
		for k15 := range value.Models {
			var mapped []domain.TariffOffer
			if value.Models[k15] != nil {
				mapped = make([]domain.TariffOffer, len(value.Models[k15]))
				for i16 := range value.Models[k15] {
					mapped[i16] = ToTariffOffer(value.Models[k15][i16])
				}
			}
			result.Models[k15] = mapped
		}
	}
	return result
}
func nativeTariffCatalog(value domain.TariffCatalog) jsplugin.PriceCatalog {
	var result jsplugin.PriceCatalog
	result.FetchedAt = value.FetchedAt
	if value.Models != nil {
		result.Models = make(map[string][]jsplugin.CatalogOffer, len(value.Models))
		for k17 := range value.Models {
			var mapped []jsplugin.CatalogOffer
			if value.Models[k17] != nil {
				mapped = make([]jsplugin.CatalogOffer, len(value.Models[k17]))
				for i18 := range value.Models[k17] {
					mapped[i18] = nativeTariffOffer(value.Models[k17][i18])
				}
			}
			result.Models[k17] = mapped
		}
	}
	return result
}
func ToVideoPriceBasis(value jsplugin.LegacyPriceBasis) domain.VideoPriceBasis {
	var result domain.VideoPriceBasis
	result.Unit = value.Unit
	result.UsageField = value.UsageField
	return result
}

func ToTariffUsageExample(value jsplugin.UsageExample) domain.TariffUsageExample {
	var result domain.TariffUsageExample
	result.Label = value.Label
	result.Facts = value.Facts
	return result
}

func ToTariffLocalizedText(value jsplugin.LocalizedText) domain.TariffLocalizedText {
	// Preserve the existing LocalizedText object-only wire representation.
	encoded, _ := value.MarshalJSON()
	return encoded
}

func ToTariffUsageField(value jsplugin.UsageFieldSchema) domain.TariffUsageField {
	var result domain.TariffUsageField
	result.Type = value.Type
	result.Unit = value.Unit
	result.Enum = value.Enum
	result.Description = map[string]string(value.Description)
	if value.EnumLabels != nil {
		result.EnumLabels = make(map[string]domain.TariffLocalizedText, len(value.EnumLabels))
		for k19 := range value.EnumLabels {
			var mapped domain.TariffLocalizedText
			mapped = ToTariffLocalizedText(value.EnumLabels[k19])
			result.EnumLabels[k19] = mapped
		}
	}
	return result
}

func ToExpressionPrice(value billingexpr.DisplayPrice) domain.ExpressionPrice {
	var result domain.ExpressionPrice
	result.Component = value.Component
	result.Unit = value.Unit
	result.AmountUSD = value.AmountUSD
	return result
}
func ToExpressionPriceTier(value billingexpr.CatalogPriceTier) domain.ExpressionPriceTier {
	var result domain.ExpressionPriceTier
	if value.Prices != nil {
		result.Prices = make([]domain.ExpressionPrice, len(value.Prices))
		for i21 := range value.Prices {
			result.Prices[i21] = ToExpressionPrice(value.Prices[i21])
		}
	}
	if value.Conditions != nil {
		result.Conditions = make([]domain.ExpressionCondition, len(value.Conditions))
		for i22 := range value.Conditions {
			result.Conditions[i22] = ToExpressionCondition(value.Conditions[i22])
		}
	}
	return result
}
func ToExpressionCondition(value billingexpr.TierCondition) domain.ExpressionCondition {
	var result domain.ExpressionCondition
	result.Var = value.Var
	result.Arg = value.Arg
	result.Op = value.Op
	result.Value = value.Value
	result.Negated = value.Negated
	if value.Operands != nil {
		result.Operands = make([]domain.ExpressionCondition, len(value.Operands))
		for i23 := range value.Operands {
			result.Operands[i23] = ToExpressionCondition(value.Operands[i23])
		}
	}
	return result
}
func nativeExpressionCondition(value domain.ExpressionCondition) billingexpr.TierCondition {
	var result billingexpr.TierCondition
	result.Var = value.Var
	result.Arg = value.Arg
	result.Op = value.Op
	result.Value = value.Value
	result.Negated = value.Negated
	if value.Operands != nil {
		result.Operands = make([]billingexpr.TierCondition, len(value.Operands))
		for i24 := range value.Operands {
			result.Operands[i24] = nativeExpressionCondition(value.Operands[i24])
		}
	}
	return result
}
func ToExpressionTier(value billingexpr.Tier) domain.ExpressionTier {
	var result domain.ExpressionTier
	result.Label = value.Label
	if value.Conditions != nil {
		result.Conditions = make([]domain.ExpressionCondition, len(value.Conditions))
		for i25 := range value.Conditions {
			result.Conditions[i25] = ToExpressionCondition(value.Conditions[i25])
		}
	}
	result.Coefficients = value.Coefficients
	result.Linear = value.Linear
	return result
}
