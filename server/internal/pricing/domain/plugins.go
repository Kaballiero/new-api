package domain

import ()

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
	PriceCatalog     *TariffCatalog
	LegacyPriceBasis map[string]VideoPriceBasis
	UsageSchema      map[string]TariffUsageField
	UsageExamples    []TariffUsageExample
}
type PluginProtocol struct{ Name string }
type PluginCandidate struct{ Plugin *Plugin }
