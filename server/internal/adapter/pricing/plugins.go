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
	v := &domain.Plugin{Meta: domain.PluginMetadata{Key: native.Meta.Key, ChannelTypes: native.Meta.ChannelTypes, PriceCatalog: native.Meta.PriceCatalog, LegacyPriceBasis: native.Meta.LegacyPriceBasis, UsageSchema: native.Meta.UsageSchema, UsageExamples: native.Meta.UsageExamples}}
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
