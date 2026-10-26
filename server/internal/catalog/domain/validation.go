package domain

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateMetadataValues(values MetadataValues, decode EndpointDecoder) error {
	if values.NameRule < NameRuleExact || values.NameRule > NameRuleSuffix {
		return errors.New("invalid metadata matching rule")
	}
	if values.Status != 0 && values.Status != 1 {
		return ErrInvalidVisibility
	}
	return ValidateModelEndpoints(values.Endpoints, decode)
}
func ValidateModelEndpoints(raw string, decode EndpointDecoder) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	endpoints, err := decode(raw)
	if err != nil {
		return fmt.Errorf("invalid endpoints: %w", err)
	}
	switch {
	case endpoints.Array:
		for _, endpoint := range endpoints.Types {
			if !endpoint.IsString || strings.TrimSpace(endpoint.Text) == "" {
				return errors.New("endpoint types must be non-empty strings")
			}
		}
	case endpoints.Object:
		for _, endpoint := range endpoints.Entries {
			if strings.TrimSpace(endpoint.Type) == "" {
				return errors.New("endpoint type is required")
			}
			switch {
			case endpoint.IsPath:
				if !strings.HasPrefix(endpoint.Path, "/") {
					return errors.New("endpoint paths must start with /")
				}
			case endpoint.IsObject:
				if !strings.HasPrefix(endpoint.Path, "/") {
					return errors.New("endpoint paths must start with /")
				}
				if endpoint.HasMethod {
					if !endpoint.MethodIsString {
						return errors.New("invalid endpoint HTTP method")
					}
					switch endpoint.Method {
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
func FindMetadataVendor(vendors map[string]*Vendor, name string) *Vendor {
	for _, vendor := range vendors {
		if strings.EqualFold(strings.TrimSpace(vendor.Name), strings.TrimSpace(name)) {
			return vendor
		}
	}
	return nil
}
