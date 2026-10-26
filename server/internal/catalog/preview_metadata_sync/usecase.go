package preview_metadata_sync

import (
	"context"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"sort"
)

type Source interface {
	Fetch(context.Context, string) (domain.SyncUpstreamSource, map[string]domain.MetadataValues, map[string]domain.Vendor, error)
}
type Codec interface {
	MetadataRecordVersion(*domain.Model, *domain.Vendor, *domain.Vendor) string
	MetadataFields(domain.MetadataValues) map[string]any
}
type Store interface {
	SyncState() (map[string]*domain.Model, map[string]*domain.Vendor, error)
	EnabledModels() []string
	ExistingNames([]string) ([]string, error)
	SyncFields() []string
}
type Usecase struct {
	store  Store
	source Source
	codec  Codec
}

func NewUsecase(store Store, source Source, codec Codec) Usecase {
	return Usecase{store: store, source: source, codec: codec}
}
func (u Usecase) Execute(ctx context.Context, input Input) (*Output, error) {
	source, upstream, upstreamVendors, err := u.source.Fetch(ctx, input.Locale)
	if err != nil {
		return nil, err
	}
	locals, vendors, err := u.store.SyncState()
	if err != nil {
		return nil, err
	}
	enabled := u.store.EnabledModels()
	missing := []string{}
	if len(enabled) > 0 {
		existing, err := u.store.ExistingNames(enabled)
		if err != nil {
			return nil, err
		}
		missing = domain.MissingNames(enabled, existing)
	}

	siteNames := make(map[string]bool)
	allNames := make(map[string]bool)
	for name := range locals {
		siteNames[name] = true
		allNames[name] = true
	}
	for _, name := range missing {
		siteNames[name] = true
		allNames[name] = true
	}
	for name := range upstream {
		allNames[name] = true
	}
	names := make([]string, 0, len(allNames))
	for name := range allNames {
		names = append(names, name)
	}
	sort.Strings(names)
	vendorByID := make(map[int]*domain.Vendor)
	for _, vendor := range vendors {
		vendorByID[vendor.Id] = vendor
	}
	candidates := make([]domain.MetadataSyncCandidate, 0, len(names))
	for _, name := range names {
		candidate := domain.MetadataSyncCandidate{ModelName: name, Scope: "catalog", Kind: "create", Fields: []domain.MetadataSyncField{}}
		if siteNames[name] {
			candidate.Scope = "site"
		}
		local := locals[name]
		up, found := upstream[name]
		if !found {
			candidate.Kind = "missing_upstream"
			candidates = append(candidates, candidate)
			continue
		}
		candidate.Upstream = &up
		var localVendor *domain.Vendor
		if local != nil {
			localVendor = vendorByID[local.VendorID]
		}
		candidate.RecordVersion = u.codec.MetadataRecordVersion(local, localVendor, domain.FindMetadataVendor(vendors, up.Vendor))
		if local != nil && local.SyncOfficial == 0 {
			candidate.Kind = "blocked"
			candidates = append(candidates, candidate)
			continue
		}
		if up.Vendor != "" && domain.FindMetadataVendor(vendors, up.Vendor) == nil {
			if _, exists := upstreamVendors[up.Vendor]; !exists {
				candidate.Kind = "missing_vendor"
				candidates = append(candidates, candidate)
				continue
			}
			candidate.VendorToCreate = up.Vendor
		}
		localValues := domain.MetadataValues{}
		if local != nil {
			candidate.Kind = "update"
			localValues = domain.MetadataValues{Description: local.Description, Icon: local.Icon, Tags: local.Tags, Endpoints: local.Endpoints, NameRule: local.NameRule, Status: local.Status}
			if localVendor != nil {
				localValues.Vendor = localVendor.Name
			}
		}
		localFields := u.codec.MetadataFields(localValues)
		upFields := u.codec.MetadataFields(up)
		for _, field := range u.store.SyncFields() {
			if local == nil || localFields[field] != upFields[field] {
				candidate.Fields = append(candidate.Fields, domain.MetadataSyncField{Field: field, Local: localFields[field], Upstream: upFields[field]})
			}
		}
		if local != nil && len(candidate.Fields) == 0 {
			candidate.Kind = "unchanged"
		}
		candidates = append(candidates, candidate)
	}
	return &Output{Source: source, Candidates: candidates}, nil
}
