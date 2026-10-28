package taskplugins

import (
	"context"
	"encoding/json"
	"time"

	"github.com/QuantumNous/new-api/common"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/setting"
)

type Runtime struct{}
type compiled struct{ plugin *jsplugin.LoadedPlugin }

func (p compiled) Metadata() domain.Meta {
	m := p.plugin.Meta
	meta := domain.Meta{
		APIVersion: m.APIVersion, Key: m.Key, Name: m.Name, Version: m.Version,
		SortPriority: m.SortPriority, Website: m.Website, Icon: m.Icon,
		Author: domain.AuthorMeta(m.Author), Auth: domain.AuthMeta(m.Auth),
		BaseURL: m.BaseURL, ChannelTypes: m.ChannelTypes, Models: m.Models,
		FetchMode: m.FetchMode, AllowedHosts: m.AllowedHosts,
		Routes: metadataJSON(m.Routes), Protocols: metadataJSON(m.Protocols),
	}
	if m.PriceCatalog != nil {
		meta.PriceCatalog = metadataJSON(m.PriceCatalog)
	}
	if len(m.Description) > 0 {
		meta.Description = metadataJSON(m.Description)
	}
	if len(m.UsageExamples) > 0 {
		meta.UsageExamples = metadataJSON(m.UsageExamples)
	}
	if len(m.LegacyPriceBasis) > 0 {
		meta.LegacyPriceBasis = metadataJSON(m.LegacyPriceBasis)
	}
	if m.UsageSchema != nil {
		meta.UsageSchema = make(map[string]domain.UsageFieldSchema, len(m.UsageSchema))
		for name, field := range m.UsageSchema {
			labels := make(map[string]map[string]string, len(field.EnumLabels))
			if field.EnumLabels == nil {
				labels = nil
			}
			for value, label := range field.EnumLabels {
				labels[value] = label
			}
			meta.UsageSchema[name] = domain.UsageFieldSchema{Type: field.Type, Unit: field.Unit,
				Enum: field.Enum, Description: field.Description, EnumLabels: labels}
		}
	}
	return meta
}

// metadataJSON encodes opaque display metadata once, without decoding it into another shape.
func metadataJSON(value any) json.RawMessage {
	raw, err := common.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func (Runtime) Register(source string, opts domain.Options) (domain.Compiled, error) {
	p, e := jsplugin.NewRegistry().Register(source, jsplugin.Options{Key: opts.Key, Version: opts.Version})
	if e != nil {
		return nil, e
	}
	return compiled{p}, nil
}
func (Runtime) RegisterFactory(source string, opts domain.Options) (domain.Compiled, error) {
	p, e := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: opts.Key, Version: opts.Version})
	if e != nil {
		return nil, e
	}
	return compiled{p}, nil
}
func (Runtime) CompilePlugin(source string, opts domain.Options) (domain.Compiled, error) {
	p, e := jsplugin.CompilePlugin(source, jsplugin.Options{Key: opts.Key, Version: opts.Version})
	if e != nil {
		return nil, e
	}
	return compiled{p}, nil
}
func (Runtime) Validate(p domain.Compiled) error {
	return jsplugin.ValidateV1Meta(p.(compiled).plugin.Meta)
}
func (Runtime) Preflight(p domain.Compiled) error {
	return jsplugin.PreflightRoutingConflict(jsplugin.DefaultRegistry.Generation(), p.(compiled).plugin)
}
func (Runtime) DecodeIcon(icon string) (string, []byte, error) {
	return jsplugin.DecodeIconDataURI(icon)
}
func (Runtime) Snapshot() domain.RegistrySnapshot {
	s := jsplugin.DefaultRegistry.Snapshot()
	out := domain.RegistrySnapshot{}
	for _, m := range s.Factory {
		out.Factory = append(out.Factory, compiled{&jsplugin.LoadedPlugin{Meta: m}}.Metadata())
	}
	for _, m := range s.Override {
		out.Override = append(out.Override, compiled{&jsplugin.LoadedPlugin{Meta: m}}.Metadata())
	}
	return out
}
func (Runtime) RoutingErrors() map[string]string { return jsplugin.DefaultRegistry.RoutingErrors() }
func (Runtime) Get(key string) (domain.Compiled, bool) {
	p, ok := jsplugin.DefaultRegistry.Get(key)
	if !ok {
		return nil, false
	}
	return compiled{p}, true
}
func (Runtime) Generation() domain.Generation {
	g := jsplugin.DefaultRegistry.Generation()
	return domain.Generation{Number: g.Number, PublishedAt: g.PublishedAt}
}
func (r Runtime) RoutingStatus() domain.RoutingStatus {
	s := jsplugin.DefaultRegistry.RoutingStatus()
	out := domain.RoutingStatus{Errors: s.Errors,
		LastRebuild: domain.RebuildOutcome{Status: s.LastRebuild.Status,
			AttemptedAt: s.LastRebuild.AttemptedAt,
			Generation:  s.LastRebuild.Generation,
			Error:       s.LastRebuild.Error}}
	if s.Generation != nil {
		out.Generation = &domain.Generation{Number: s.Generation.Number, PublishedAt: s.Generation.PublishedAt}
	}
	return out
}
func (Runtime) OverridePlugins() map[string]domain.Compiled {
	out := map[string]domain.Compiled{}
	for k, p := range jsplugin.DefaultRegistry.OverridePlugins() {
		out[k] = compiled{p}
	}
	return out
}
func (Runtime) ReplaceOverrides(values []domain.Compiled) error {
	out := make([]*jsplugin.LoadedPlugin, 0, len(values))
	for _, p := range values {
		out = append(out, p.(compiled).plugin)
	}
	return jsplugin.DefaultRegistry.ReplaceOverrides(out)
}
func (Runtime) ActiveCount() int { return len(jsplugin.DefaultRegistry.ActiveOverridePlugins()) }
func (Runtime) Call(p domain.Compiled, hook, member string, args []any) (any, error) {
	engine := p.(compiled).plugin.Engine
	if member == "" {
		return engine.Call(context.Background(), hook, args...)
	}
	return engine.CallMember(context.Background(), hook, member, args...)
}

type Settings struct {
	WriteOption  func(string, string) error
	ChangeStatus func(int, string, int, string) bool
}

func (Settings) FactoryDisabled(key string) bool               { return setting.IsTaskPluginFactoryDisabled(key) }
func (Settings) FactorySource(key string) (string, error)      { return plugins.Source(key) }
func (Settings) FactoryIcon(key string) (string, []byte, bool) { return plugins.Icon(key) }
func (Settings) FactoryIconURI(key string) string              { return plugins.IconDataURI(key) }
func (Settings) DisabledKeys() []string                        { return setting.GetTaskPluginDisabledFactoryKeys() }
func (Settings) SetDisabledKeys(keys []string) error {
	return setting.SetTaskPluginDisabledFactoryKeysOption(keys)
}
func (s Settings) SaveDisabledKeys() error {
	raw, e := common.Marshal(setting.GetTaskPluginDisabledFactoryKeys())
	if e != nil {
		return e
	}
	return s.WriteOption(setting.TaskPluginDisabledFactoryKeysKey, string(raw))
}
func (Settings) Sources() []domain.MarketplaceSource {
	v := setting.GetTaskPluginMarketplaceSources()
	out := make([]domain.MarketplaceSource, len(v))
	for i := range v {
		out[i] = domain.MarketplaceSource(v[i])
	}
	return out
}
func (s Settings) SaveSources(v []domain.MarketplaceSource) error {
	raw, e := common.Marshal(v)
	if e != nil {
		return e
	}
	return s.WriteOption(setting.TaskPluginMarketplaceSourcesKey, string(raw))
}
func (settings Settings) DisableChannel(id int) bool {
	return settings.ChangeStatus(id, "", common.ChannelStatusManuallyDisabled, "task plugin disabled")
}
func (Settings) InvalidatePricing() { pricingstate.InvalidatePricingCache() }

type Diagnostics struct{}

func (Diagnostics) Debug(ctx context.Context, format string, args ...any) {
	logger.LogDebug(ctx, format, args...)
}
func (Diagnostics) Error(message string) { common.SysError(message) }
func (Diagnostics) Now() time.Time       { return time.Now() }

type Decoder struct{}

func (Decoder) Argument(raw json.RawMessage, v *any) error { return common.Unmarshal(raw, v) }
