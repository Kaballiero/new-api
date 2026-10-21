package domain

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"strings"
)

func ValidateMetadataValues(values MetadataValues) error {
	if values.NameRule < NameRuleExact || values.NameRule > NameRuleSuffix {
		return errors.New("invalid metadata matching rule")
	}
	if values.Status != 0 && values.Status != 1 {
		return ErrInvalidVisibility
	}
	return ValidateModelEndpoints(values.Endpoints)
}
func ValidateModelEndpoints(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(raw, &value); err != nil {
		return fmt.Errorf("invalid endpoints: %w", err)
	}
	switch endpoints := value.(type) {
	case []any:
		for _, endpoint := range endpoints {
			if text, ok := endpoint.(string); !ok || strings.TrimSpace(text) == "" {
				return errors.New("endpoint types must be non-empty strings")
			}
		}
	case map[string]any:
		for key, endpoint := range endpoints {
			if strings.TrimSpace(key) == "" {
				return errors.New("endpoint type is required")
			}
			switch details := endpoint.(type) {
			case string:
				if !strings.HasPrefix(details, "/") {
					return errors.New("endpoint paths must start with /")
				}
			case map[string]any:
				path, _ := details["path"].(string)
				if !strings.HasPrefix(path, "/") {
					return errors.New("endpoint paths must start with /")
				}
				if method, exists := details["method"]; exists {
					switch method {
					case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
					default:
						return errors.New("invalid endpoint HTTP method")
					}
				}
			default:
				return errors.New("endpoint configuration must be a path or object")
			}
		}
	default:
		return errors.New("endpoints must be a JSON object or array")
	}
	return nil
}
func MetadataRecordVersion(local *Model, localVendor, upstreamVendor *Vendor) string {
	encoded, _ := common.Marshal([]any{local, localVendor, upstreamVendor})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
func VendorRecordVersion(v *Vendor) string {
	encoded, _ := common.Marshal([]any{v.Id, v.Name, v.Description, v.Icon, v.Status, v.CreatedTime, v.UpdatedTime})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
func FindMetadataVendor(vendors map[string]*Vendor, name string) *Vendor {
	for _, vendor := range vendors {
		if strings.EqualFold(strings.TrimSpace(vendor.Name), strings.TrimSpace(name)) {
			return vendor
		}
	}
	return nil
}
