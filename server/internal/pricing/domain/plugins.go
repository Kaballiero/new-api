package domain

import (
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

// PluginGeneration exposes the read-only facts needed for tariff projection.
// Runtime engines and registry publication stay in the technical adapter.
type PluginGeneration interface {
	Number() uint64
	Get(string) (*Plugin, bool)
	GetByChannelType(int) (*Plugin, bool)
	GetByModel(string) (*Plugin, bool)
	CanonicalModel(string) (string, bool)
	LookupEndpointCandidates(string, string, string) []PluginCandidate
}
type Plugin struct{ Meta PluginMetadata }
type PluginMetadata struct {
	Key              string
	ChannelTypes     []int
	Protocols        []PluginProtocol
	PriceCatalog     *jsplugin.PriceCatalog
	LegacyPriceBasis map[string]jsplugin.LegacyPriceBasis
	UsageSchema      map[string]jsplugin.UsageFieldSchema
	UsageExamples    []jsplugin.UsageExample
}
type PluginProtocol struct{ Name string }
type PluginCandidate struct{ Plugin *Plugin }
