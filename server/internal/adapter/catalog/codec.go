package catalog

import (
	"crypto/sha256"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Codec struct{}

func (Codec) DecodeEndpoints(raw string) (domain.EndpointDocument, error) {
	var value any
	if err := common.UnmarshalJsonStr(raw, &value); err != nil {
		return domain.EndpointDocument{}, err
	}
	var out domain.EndpointDocument
	switch endpoints := value.(type) {
	case []any:
		out.Array = true
		for _, endpoint := range endpoints {
			text, ok := endpoint.(string)
			out.Types = append(out.Types, domain.EndpointTypeFact{Text: text, IsString: ok})
		}
	case map[string]any:
		out.Object = true
		for key, endpoint := range endpoints {
			entry := domain.EndpointEntry{Type: key}
			switch details := endpoint.(type) {
			case string:
				entry.IsPath, entry.Path = true, details
			case map[string]any:
				entry.IsObject = true
				entry.Path, _ = details["path"].(string)
				method, exists := details["method"]
				entry.HasMethod = exists
				entry.Method, entry.MethodIsString = method.(string)
			}
			out.Entries = append(out.Entries, entry)
		}
	}
	return out, nil
}
func (Codec) MetadataRecordVersion(local *domain.Model, localVendor, upstreamVendor *domain.Vendor) string {
	encoded, _ := common.Marshal([]any{local, localVendor, upstreamVendor})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
func (Codec) VendorRecordVersion(v *domain.Vendor) string {
	encoded, _ := common.Marshal([]any{v.Id, v.Name, v.Description, v.Icon, v.Status, v.CreatedTime, v.UpdatedTime})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
func (Codec) VendorOperationVersion(preview *domain.VendorOperationPreview, modelVersions []string) (string, error) {
	encoded, err := common.Marshal([]any{preview, modelVersions})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}
func (Codec) MetadataFields(values domain.MetadataValues) map[string]any {
	encoded, _ := common.Marshal(values)
	var fields map[string]any
	_ = common.Unmarshal(encoded, &fields)
	return fields
}
