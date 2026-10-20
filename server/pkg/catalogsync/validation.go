package catalogsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
)

// The wire contract uses closed objects, explicit nullable fields and required
// arrays. Go zero values alone cannot distinguish a missing price from null.
func wireObject(raw []byte, nullable []string, fields ...string) (map[string]json.RawMessage, error) {
	if common.GetJsonType(raw) != "object" || common.HasDuplicateJSONKeys(raw) {
		return nil, fmt.Errorf("catalog object has invalid type or duplicate keys")
	}
	var object map[string]json.RawMessage
	if err := common.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	if len(object) != len(fields) {
		return nil, fmt.Errorf("catalog object has missing or unknown fields")
	}
	for _, field := range fields {
		value, exists := object[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !slices.Contains(nullable, field) {
			return nil, fmt.Errorf("missing or null catalog field %s", field)
		}
	}
	return object, nil
}
func wireStringArray(raw []byte) error {
	if common.GetJsonType(raw) != "array" {
		return fmt.Errorf("catalog string array required")
	}
	var values []json.RawMessage
	if err := common.Unmarshal(raw, &values); err != nil {
		return err
	}
	for _, value := range values {
		if common.GetJsonType(value) != "string" {
			return fmt.Errorf("catalog string array contains non-string")
		}
	}
	return nil
}
func wireCoverage(raw []byte) error {
	object, err := wireObject(raw, nil, "offers", "normalization", "reasons", "expectedSources", "missingSources")
	if err != nil {
		return err
	}
	var coverage Coverage
	if err := common.Unmarshal(raw, &coverage); err != nil {
		return err
	}
	for _, state := range []string{coverage.Offers, coverage.Normalization} {
		if !slices.Contains([]string{"complete", "partial", "unknown"}, state) {
			return fmt.Errorf("invalid catalog coverage state")
		}
	}
	for _, key := range []string{"reasons", "expectedSources", "missingSources"} {
		if err := wireStringArray(object[key]); err != nil {
			return err
		}
	}
	return nil
}
func validateDocumentShape(raw []byte) error {
	root, err := wireObject(raw, []string{"description", "author", "alias"}, "schemaVersion", "id", "name", "description", "author", "type", "modalities", "metadata", "capabilities", "limits", "alias", "offers", "coverage", "limitations", "provenance")
	if err != nil {
		return err
	}
	if err := wireCoverage(root["coverage"]); err != nil {
		return err
	}
	if err := wireStringArray(root["limitations"]); err != nil {
		return err
	}
	modalities, err := wireObject(root["modalities"], nil, "input", "output")
	if err != nil {
		return err
	}
	for _, key := range []string{"input", "output"} {
		if err := wireStringArray(modalities[key]); err != nil {
			return err
		}
	}
	for _, key := range []string{"author", "alias"} {
		if common.GetJsonType(root[key]) == "null" {
			continue
		}
		field := "namespace"
		if key == "alias" {
			field = "targetId"
		}
		if _, err := wireObject(root[key], nil, field); err != nil {
			return err
		}
	}
	var offers []json.RawMessage
	if common.GetJsonType(root["offers"]) != "array" {
		return fmt.Errorf("catalog offers array required")
	}
	if err := common.Unmarshal(root["offers"], &offers); err != nil {
		return err
	}
	for _, rawOffer := range offers {
		offer, err := wireObject(rawOffer, []string{"provider", "operation", "mode", "eligibility"}, "id", "scope", "identity", "provider", "operation", "mode", "variant", "capabilities", "limits", "eligibility", "components", "coverage", "limitations", "facts")
		if err != nil {
			return err
		}
		if err := wireCoverage(offer["coverage"]); err != nil {
			return err
		}
		if err := wireStringArray(offer["limitations"]); err != nil {
			return err
		}
		for _, key := range []string{"identity", "variant", "capabilities", "limits", "facts"} {
			if common.GetJsonType(offer[key]) != "object" {
				return fmt.Errorf("catalog offer %s must be an object", key)
			}
		}
		if common.GetJsonType(offer["provider"]) != "null" {
			if _, err := wireObject(offer["provider"], []string{"id", "name", "tag"}, "id", "name", "tag"); err != nil {
				return err
			}
		}
		if common.GetJsonType(offer["eligibility"]) != "null" {
			eligibility, err := wireObject(offer["eligibility"], nil, "eligible", "reasons")
			if err != nil {
				return err
			}
			if err := wireStringArray(eligibility["reasons"]); err != nil {
				return err
			}
		}
		if common.GetJsonType(offer["components"]) != "array" {
			return fmt.Errorf("catalog components array required")
		}
		var components []json.RawMessage
		if err := common.Unmarshal(offer["components"], &components); err != nil {
			return err
		}
		for _, rawComponent := range components {
			component, err := wireObject(rawComponent, []string{"billable", "direction", "modality", "amountUSD"}, "parameter", "componentId", "billable", "direction", "modality", "amountUSD", "unit", "quantity", "variant", "conditions", "source", "provenance", "normalization")
			if err != nil {
				return err
			}
			if _, err := wireObject(component["source"], []string{"amount", "currency", "unit"}, "amount", "currency", "unit", "key"); err != nil {
				return err
			}
			normalized, err := wireObject(component["normalization"], []string{"appliedDiscount"}, "comparable", "effective", "reasons", "discountBasis", "appliedDiscount")
			if err != nil {
				return err
			}
			var basis string
			if err := common.Unmarshal(normalized["discountBasis"], &basis); err != nil {
				return err
			}
			if !slices.Contains([]string{"gross", "net", "unknown"}, basis) {
				return fmt.Errorf("invalid catalog discount basis")
			}
			if err := wireStringArray(normalized["reasons"]); err != nil {
				return err
			}
		}
	}
	return nil
}
