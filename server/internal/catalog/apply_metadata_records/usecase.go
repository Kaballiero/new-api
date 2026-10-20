package apply_metadata_records

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"strings"
)

type Store interface {
	WithMetadataSync(func(domain.MetadataMutation) error) error
	SyncFields() []string
	SyncConflict() error
}
type Usecase struct {
	store   Store
	now     func() int64
	refresh func()
}

func NewUsecase(store Store, now func() int64, refresh func()) Usecase {
	return Usecase{store: store, now: now, refresh: refresh}
}
func (u Usecase) Execute(updates []domain.MetadataSyncUpdate, upstreamVendors map[string]domain.Vendor) (*domain.MetadataSyncResult, error) {
	if len(updates) == 0 {
		return nil, errors.New("select metadata changes before applying")
	}
	seen := make(map[string]bool)
	for _, update := range updates {
		if seen[update.ModelName] || strings.TrimSpace(update.ModelName) == "" {
			return nil, errors.New("invalid or duplicate model selection")
		}
		seen[update.ModelName] = true
		if update.RecordVersion == "" {
			return nil, u.store.SyncConflict()
		}
		if err := domain.ValidateMetadataValues(update.Values); err != nil {
			return nil, err
		}
		if !update.Create && len(update.Fields) == 0 {
			return nil, errors.New("select fields to update")
		}
		allowed := make(map[string]bool)
		for _, field := range u.store.SyncFields() {
			allowed[field] = true
		}
		for _, field := range update.Fields {
			if !allowed[field] {
				return nil, fmt.Errorf("unsupported metadata field: %s", field)
			}
		}
	}
	result := &domain.MetadataSyncResult{CreatedModels: []string{}, UpdatedModels: []domain.MetadataSyncSelection{}, CreatedVendors: []string{}}
	err := u.store.WithMetadataSync(func(tx domain.MetadataMutation) error {
		locals, vendors, err := tx.State()
		if err != nil {
			return err
		}
		vendorByID := make(map[int]*domain.Vendor)
		for _, vendor := range vendors {
			vendorByID[vendor.Id] = vendor
		}
		// Verify the entire selection before any write, including shared vendors.
		for _, update := range updates {
			local := locals[update.ModelName]
			if local != nil && local.SyncOfficial == 0 {
				return fmt.Errorf("metadata sync is disabled for %s", update.ModelName)
			}
			var localVendor *domain.Vendor
			if local != nil {
				localVendor = vendorByID[local.VendorID]
			}
			if domain.MetadataRecordVersion(local, localVendor, domain.FindMetadataVendor(vendors, update.Values.Vendor)) != update.RecordVersion {
				return fmt.Errorf("%w: %s", u.store.SyncConflict(), update.ModelName)
			}
			if update.Create && local != nil || !update.Create && local == nil {
				return u.store.SyncConflict()
			}
		}
		for _, update := range updates {
			fields := make(map[string]any)
			selected := update.Fields
			if update.Create {
				selected = u.store.SyncFields()
			}
			for _, field := range selected {
				switch field {
				case "description":
					fields[field] = update.Values.Description
				case "icon":
					fields[field] = update.Values.Icon
				case "tags":
					fields[field] = update.Values.Tags
				case "endpoints":
					fields[field] = update.Values.Endpoints
				case "name_rule":
					fields[field] = update.Values.NameRule
				case "status":
					fields[field] = update.Values.Status
				case "vendor":
					vendorID := 0
					name := update.Values.Vendor
					if name != "" {
						vendor := domain.FindMetadataVendor(vendors, name)
						if vendor == nil {
							up, ok := upstreamVendors[name]
							if !ok {
								return fmt.Errorf("upstream vendor not found: %s", name)
							}
							vendor = &domain.Vendor{Name: name, Description: up.Description, Icon: up.Icon, Status: 1, CreatedTime: u.now(), UpdatedTime: u.now()}
							if err := domain.NormalizeVendor(vendor); err != nil {
								return err
							}
							others, err := tx.OtherVendors(vendor.Id)
							if err != nil {
								return err
							}
							if err := domain.ValidateVendorName(vendor, others); err != nil {
								return err
							}
							if err := tx.InsertVendor(vendor); err != nil {
								return err
							}
							vendors[name] = vendor
							result.CreatedVendors = append(result.CreatedVendors, name)
						}
						vendorID = vendor.Id
					}
					fields["vendor_id"] = vendorID
				}
			}
			fields["updated_time"] = u.now()
			if update.Create {
				fields["model_name"] = update.ModelName
				fields["sync_official"] = 1
				fields["created_time"] = u.now()
				if err := tx.InsertModel(fields); err != nil {
					return err
				}
				result.CreatedModels = append(result.CreatedModels, update.ModelName)
			} else {
				if err := tx.UpdateModel(locals[update.ModelName].Id, fields); err != nil {
					return err
				}
				result.UpdatedModels = append(result.UpdatedModels, update.MetadataSyncSelection)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	u.refresh()
	return result, nil
}
