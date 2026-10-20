package record

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

func ToModel(value *Model) *domain.Model {
	if value == nil {
		return nil
	}
	out := &domain.Model{Id: value.Id, ModelName: value.ModelName, Description: value.Description, Icon: value.Icon, Tags: value.Tags, VendorID: value.VendorID, Endpoints: value.Endpoints, SupportedEndpoints: value.SupportedEndpoints, Status: value.Status, SyncOfficial: value.SyncOfficial, CreatedTime: value.CreatedTime, UpdatedTime: value.UpdatedTime, EnableGroups: value.EnableGroups, QuotaTypes: value.QuotaTypes, NameRule: value.NameRule, MatchedModels: value.MatchedModels, MatchedCount: value.MatchedCount, HasMetadata: value.HasMetadata, ConfiguredChannelCount: value.ConfiguredChannelCount, SquareState: domain.ModelSquareState(value.SquareState)}
	out.BoundChannels = value.BoundChannels
	return out
}
func FromModel(value *domain.Model) *Model {
	if value == nil {
		return nil
	}
	out := &Model{Id: value.Id, ModelName: value.ModelName, Description: value.Description, Icon: value.Icon, Tags: value.Tags, VendorID: value.VendorID, Endpoints: value.Endpoints, SupportedEndpoints: value.SupportedEndpoints, Status: value.Status, SyncOfficial: value.SyncOfficial, CreatedTime: value.CreatedTime, UpdatedTime: value.UpdatedTime, EnableGroups: value.EnableGroups, QuotaTypes: value.QuotaTypes, NameRule: value.NameRule, MatchedModels: value.MatchedModels, MatchedCount: value.MatchedCount, HasMetadata: value.HasMetadata, ConfiguredChannelCount: value.ConfiguredChannelCount, SquareState: ModelSquareState(value.SquareState)}
	out.BoundChannels = value.BoundChannels
	return out
}
func ToVendor(value *Vendor) *domain.Vendor {
	if value == nil {
		return nil
	}
	out := &domain.Vendor{ModelCount: value.ModelCount, Version: value.Version, Id: value.Id, Name: value.Name, Description: value.Description, Icon: value.Icon, Status: value.Status, CreatedTime: value.CreatedTime, UpdatedTime: value.UpdatedTime}
	return out
}
func FromVendor(value *domain.Vendor) *Vendor {
	if value == nil {
		return nil
	}
	out := &Vendor{ModelCount: value.ModelCount, Version: value.Version, Id: value.Id, Name: value.Name, Description: value.Description, Icon: value.Icon, Status: value.Status, CreatedTime: value.CreatedTime, UpdatedTime: value.UpdatedTime}
	return out
}
func ModelsToDomain(values []*Model) []*domain.Model {
	if values == nil {
		return nil
	}
	out := make([]*domain.Model, len(values))
	for i, v := range values {
		out[i] = ToModel(v)
	}
	return out
}
func VendorsToDomain(values []*Vendor) []*domain.Vendor {
	if values == nil {
		return nil
	}
	out := make([]*domain.Vendor, len(values))
	for i, v := range values {
		out[i] = ToVendor(v)
	}
	return out
}
