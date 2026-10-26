package catalog

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/modelcatalog"
)

type Models struct{}

func (Models) Profile(name string) domain.AvailableModel {
	v, ok := modelcatalog.Lookup(name)
	if !ok {
		return domain.AvailableModel{Id: name, Object: "model", Created: 1626777600, OwnedBy: "custom", SupportedEndpointTypes: endpoints(name)}
	}
	return domain.AvailableModel{Id: v.Id, Object: v.Object, Created: v.Created, OwnedBy: v.OwnedBy, SupportedEndpointTypes: endpoints(name)}
}
func (Models) Registered(name string) (domain.AvailableModel, bool) {
	v, ok := modelcatalog.Lookup(name)
	if !ok {
		return domain.AvailableModel{}, false
	}
	return domain.AvailableModel{Id: v.Id, Object: v.Object, Created: v.Created, OwnedBy: v.OwnedBy}, true
}
func (Models) Owner(channel int) string    { return modelcatalog.OwnerName(channel) }
func (Models) Configured(name string) bool { return helper.HasModelBillingConfig(name) }
func (Models) Presets() map[int][]string   { return modelcatalog.Presets() }
func endpoints(name string) []string {
	values := model.GetModelSupportEndpointTypes(name)
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}
