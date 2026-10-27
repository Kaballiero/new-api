package pricing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/catalogsync"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type Expressions struct{}

func (Expressions) ParseExprVersion(value string) (int, string) {
	return billingexpr.ParseExprVersion(value)
}
func (Expressions) UsedVars(value string) map[string]bool { return billingexpr.UsedVars(value) }
func (Expressions) CatalogPrices(value string) [][]domain.ExpressionPrice {
	native := billingexpr.CatalogPrices(value)
	if native == nil {
		return nil
	}
	result := make([][]domain.ExpressionPrice, len(native))
	for i, row := range native {
		if row != nil {
			result[i] = make([]domain.ExpressionPrice, len(row))
			for j, price := range row {
				result[i][j] = ToExpressionPrice(price)
			}
		}
	}
	return result
}
func (Expressions) CatalogPriceTiers(value string) []domain.ExpressionPriceTier {
	native := billingexpr.CatalogPriceTiers(value)
	if native == nil {
		return nil
	}
	result := make([]domain.ExpressionPriceTier, len(native))
	for i, tier := range native {
		result[i] = ToExpressionPriceTier(tier)
	}
	return result
}
func (Expressions) EnumerateTiers(value string) ([]domain.ExpressionTier, error) {
	native, err := billingexpr.EnumerateTiers(value)
	if native == nil {
		return nil, err
	}
	result := make([]domain.ExpressionTier, len(native))
	for i, tier := range native {
		result[i] = ToExpressionTier(tier)
	}
	return result, err
}

type Catalog struct{}

func (Catalog) DecodeDocument(raw []byte, ref domain.SourceReference) (*domain.SourceDocument, error) {
	value, err := catalogsync.DecodeDocument(raw, nativeSourceReference(ref))
	if value == nil {
		return nil, err
	}
	result := ToSourceDocument(*value)
	return &result, err
}
func (Catalog) Aliases(documents map[string]*domain.SourceDocument) func(string) (*domain.SourceDocument, error) {
	native := make(map[string]*catalogsync.Document, len(documents))
	originals := make(map[*catalogsync.Document]*domain.SourceDocument, len(documents))
	for key, document := range documents {
		if document == nil {
			native[key] = nil
			continue
		}
		value := nativeSourceDocument(*document)
		native[key] = &value
		originals[&value] = document
	}
	return func(id string) (*domain.SourceDocument, error) {
		resolved, err := catalogsync.ResolveAlias(native, id)
		return originals[resolved], err
	}
}
func (Catalog) ModelTypes() []string       { return catalogsync.ModelTypes() }
func (Catalog) CompareIDs(a, b string) int { return catalogsync.CompareIDs(a, b) }
func (Catalog) ValidatePriceCatalog(value domain.TariffCatalog, models []string) error {
	native := nativeTariffCatalog(value)
	return native.Validate(models)
}

type Endpoints struct{}

func (Endpoints) GetEndpointTypesByChannelType(kind int, model string) []constant.EndpointType {
	return common.GetEndpointTypesByChannelType(kind, model)
}
func (Endpoints) GetDefaultEndpointInfo(endpoint constant.EndpointType) (domain.PricingEndpointInfo, bool) {
	value, ok := common.GetDefaultEndpointInfo(endpoint)
	return domain.PricingEndpointInfo(value), ok
}
func (Endpoints) AudioCharacterCount(model string) bool {
	return (&dto.AudioRequest{Model: model}).GetTokenCountMeta().TokenType == types.TokenTypeTextNumber
}
func (Endpoints) AudioTokenizerCount(model string) bool {
	return (&dto.AudioRequest{Model: model}).GetTokenCountMeta().TokenType == types.TokenTypeTokenizer
}
func Capabilities() domain.Capabilities {
	return domain.Capabilities{
		Codec: domain.JSONCodec{
			DecodeCatalogSnapshot: decodeCatalogSnapshot,
			Marshal:               common.Marshal,
			Unmarshal:             common.Unmarshal,
			UnmarshalJsonStr:      common.UnmarshalJsonStr,
			DecodeJson:            common.DecodeJson,
			GetJsonType:           common.GetJsonType,
		},
		Expressions: Expressions{}, Catalog: Catalog{}, Endpoints: Endpoints{},
	}
}

type ChannelCodec struct{}

func (ChannelCodec) Setting(raw string, result *domain.PricingChannelSetting) error {
	var native dto.ChannelSettings
	err := common.UnmarshalJsonStr(raw, &native)
	result.PassThroughBodyEnabled = native.PassThroughBodyEnabled
	result.TaskPluginKey = native.TaskPluginKey
	return err
}
func (ChannelCodec) Other(raw string, result *domain.PricingChannelOther) error {
	var native dto.ChannelOtherSettings
	err := common.UnmarshalJsonStr(raw, &native)
	result.AllowServiceTier = native.AllowServiceTier
	if native.AdvancedCustom != nil {
		result.AdvancedCustom = native.AdvancedCustom
	}
	return err
}
func AdvancedConfigurations(values map[int]*dto.AdvancedCustomConfig) map[int]domain.AdvancedEndpoints {
	if values == nil {
		return nil
	}
	result := make(map[int]domain.AdvancedEndpoints, len(values))
	for id, value := range values {
		if value != nil {
			result[id] = value
		}
	}
	return result
}
