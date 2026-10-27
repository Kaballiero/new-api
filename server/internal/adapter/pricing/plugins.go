package pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

type Generation struct {
	native  *jsplugin.RoutingGeneration
	plugins map[*jsplugin.LoadedPlugin]*domain.Plugin
}

func NewGeneration(native *jsplugin.RoutingGeneration) *Generation {
	return &Generation{native: native, plugins: make(map[*jsplugin.LoadedPlugin]*domain.Plugin)}
}
func (g *Generation) Native() *jsplugin.RoutingGeneration { return g.native }
func (g *Generation) Number() uint64                      { return g.native.Number }
func (g *Generation) plugin(native *jsplugin.LoadedPlugin) *domain.Plugin {
	if native == nil {
		return nil
	}
	if v, ok := g.plugins[native]; ok {
		return v
	}
	v := &domain.Plugin{Meta: domain.PluginMetadata{Key: native.Meta.Key, ChannelTypes: native.Meta.ChannelTypes}}
	if native.Meta.PriceCatalog != nil {
		value := ToTariffCatalog(*native.Meta.PriceCatalog)
		v.Meta.PriceCatalog = &value
	}
	if native.Meta.LegacyPriceBasis != nil {
		v.Meta.LegacyPriceBasis = make(map[string]domain.VideoPriceBasis, len(native.Meta.LegacyPriceBasis))
		for key, value := range native.Meta.LegacyPriceBasis {
			v.Meta.LegacyPriceBasis[key] = ToVideoPriceBasis(value)
		}
	}
	v.Meta.UsageSchema = UsageSchema(native.Meta.UsageSchema)
	if native.Meta.UsageExamples != nil {
		v.Meta.UsageExamples = make([]domain.TariffUsageExample, len(native.Meta.UsageExamples))
		for i, value := range native.Meta.UsageExamples {
			v.Meta.UsageExamples[i] = ToTariffUsageExample(value)
		}
	}
	for _, p := range native.Meta.Protocols {
		v.Meta.Protocols = append(v.Meta.Protocols, domain.PluginProtocol{Name: p.Name})
	}
	g.plugins[native] = v
	return v
}
func (g *Generation) Get(key string) (*domain.Plugin, bool) {
	p, ok := g.native.Get(key)
	return g.plugin(p), ok
}
func (g *Generation) GetByChannelType(kind int) (*domain.Plugin, bool) {
	p, ok := g.native.GetByChannelType(kind)
	return g.plugin(p), ok
}
func (g *Generation) GetByModel(name string) (*domain.Plugin, bool) {
	p, ok := g.native.GetByModel(name)
	return g.plugin(p), ok
}
func (g *Generation) CanonicalModel(name string) (string, bool) { return g.native.CanonicalModel(name) }
func (g *Generation) LookupEndpointCandidates(method, path, model string) []domain.PluginCandidate {
	rows := g.native.LookupEndpointCandidates(method, path, model)
	out := make([]domain.PluginCandidate, len(rows))
	for i, v := range rows {
		out[i] = domain.PluginCandidate{Plugin: g.plugin(v.Plugin)}
	}
	return out
}

func UsageSchema(value map[string]jsplugin.UsageFieldSchema) map[string]domain.TariffUsageField {
	if value == nil {
		return nil
	}
	result := make(map[string]domain.TariffUsageField, len(value))
	for key, field := range value {
		result[key] = ToTariffUsageField(field)
	}
	return result
}
