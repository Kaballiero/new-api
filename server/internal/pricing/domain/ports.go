package domain

import (
	"encoding/json"
	"io"

	"github.com/QuantumNous/new-api/constant"
)

// JSONCodec preserves the configured host serializer, including partial decoding.
type JSONCodec struct {
	DecodeCatalogSnapshot func(string, *PricingCatalogSnapshot) error
	Marshal               func(any) ([]byte, error)
	Unmarshal             func([]byte, any) error
	UnmarshalJsonStr      func(string, any) error
	DecodeJson            func(io.Reader, any) error
	GetJsonType           func(json.RawMessage) string
}

// ExpressionReader exposes display facts from the existing expression engine.
type ExpressionReader interface {
	CatalogPrices(string) [][]ExpressionPrice
	CatalogPriceTiers(string) []ExpressionPriceTier
	EnumerateTiers(string) ([]ExpressionTier, error)
	ParseExprVersion(string) (int, string)
	UsedVars(string) map[string]bool
}
type CatalogReader interface {
	DecodeDocument([]byte, SourceReference) (*SourceDocument, error)
	Aliases(map[string]*SourceDocument) func(string) (*SourceDocument, error)
	ModelTypes() []string
	CompareIDs(string, string) int
	ValidatePriceCatalog(TariffCatalog, []string) error
}
type PricingEndpointInfo struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}
type AdvancedEndpoints interface {
	SupportedEndpointTypesForModel(string) []constant.EndpointType
}
type EndpointRules interface {
	GetEndpointTypesByChannelType(int, string) []constant.EndpointType
	GetDefaultEndpointInfo(constant.EndpointType) (PricingEndpointInfo, bool)
	AudioCharacterCount(string) bool
	AudioTokenizerCount(string) bool
}

// Capabilities are stateless technical dependencies of pricing projection.
// Publication, registry and expression caches retain their existing owners.
type Capabilities struct {
	Codec       JSONCodec
	Expressions ExpressionReader
	Catalog     CatalogReader
	Endpoints   EndpointRules
}

type PricingChannelSetting struct {
	PassThroughBodyEnabled bool
	TaskPluginKey          string
}
type PricingChannelOther struct {
	AllowServiceTier bool
	AdvancedCustom   AdvancedEndpoints
}
