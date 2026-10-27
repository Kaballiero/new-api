package domain

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

type PricingValues map[string]any
type ModelPricingChange struct {
	ModelName       string        `json:"model_name"`
	ExpectedVersion string        `json:"expected_version"`
	Pricing         PricingValues `json:"pricing"`
	Reset           bool          `json:"reset,omitempty"`
}
type ModelPricingEntry struct {
	ModelName   string                      `json:"model_name"`
	Version     string                      `json:"version"`
	Configured  PricingValues               `json:"configured"`
	Effective   PricingValues               `json:"effective"`
	UsageSchema map[string]TariffUsageField `json:"usage_schema,omitempty"`
}
type ModelPricingSnapshot struct {
	Entries      []ModelPricingEntry `json:"entries"`
	Options      map[string]string   `json:"options"`
	EmptyVersion string              `json:"empty_version"`
}
type OptionRow struct{ Key, Value string }
type ConfigurationRuntime struct {
	Capabilities            Capabilities
	Keys                    func() []string
	DefaultMaps             func() map[string]map[string]float64
	MatchingName            func(string) string
	Builtin                 func(string) (string, bool)
	Builtins                func() map[string]string
	SelfUse                 func() bool
	Completion              func(string) (float64, bool)
	ValidateExpression      func(string, string) error
	ValidateBatchExpression func(string) error
}
type Configuration struct{ runtime ConfigurationRuntime }

func NewConfiguration(runtime ConfigurationRuntime) *Configuration {
	return &Configuration{runtime: runtime}
}
func (c *Configuration) Keys() []string              { return c.runtime.Keys() }
func (c *Configuration) Builtins() map[string]string { return c.runtime.Builtins() }
func (c *Configuration) IsOption(key string) bool {
	return slices.Contains(c.runtime.Keys(), key)
}
func ModelPricingVersion(deps Capabilities, values PricingValues) string {
	encoded, _ := deps.Codec.Marshal(values)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
func (c *Configuration) Defaults() map[string]map[string]any {
	result := make(map[string]map[string]any, len(c.runtime.Keys()))
	for _, key := range c.runtime.Keys() {
		result[key] = make(map[string]any)
	}
	for key, values := range c.runtime.DefaultMaps() {
		for name, value := range values {
			result[key][name] = value
		}
	}
	return result
}
func (c *Configuration) Values(values map[string]map[string]any, name string) PricingValues {
	result := make(PricingValues)
	for _, key := range c.runtime.Keys() {
		if value, exists := values[key][name]; exists {
			result[key] = value
		}
	}
	return result
}
func (c *Configuration) Effective(values map[string]map[string]any, name string) PricingValues {
	result := c.Values(values, name)

	alias := c.runtime.MatchingName(name)
	for _, key := range c.runtime.Keys()[:8] {
		if value, exists := values[key][alias]; exists {
			result[key] = value
		}
	}
	mode, _ := result["billing_setting.billing_mode"].(string)
	if mode == "" {
		_, hasPrice := result["ModelPrice"]
		_, hasRatio := result["ModelRatio"]
		if _, builtin := c.runtime.Builtin(name); builtin && !hasPrice && !hasRatio {
			mode = "tiered_expr"
		}
	}
	if mode == "tiered_expr" {
		result["billing_setting.billing_mode"] = mode
		if _, exists := result["billing_setting.billing_expr"]; !exists {
			if expression, ok := c.runtime.Builtin(name); ok {
				result["billing_setting.billing_expr"] = expression
			}
		}
		return result
	}
	if _, exists := result["ModelPrice"]; exists {
		return result
	}
	if _, exists := result["ModelRatio"]; !exists && c.runtime.SelfUse() {
		result["ModelRatio"] = float64(37.5)
	}

	ratio, locked := c.runtime.Completion(name)
	if _, exists := result["CompletionRatio"]; !exists || locked {
		result["CompletionRatio"] = ratio
	}
	return result
}
func (c *Configuration) Validate(name string, values PricingValues) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("model name is required")
	}
	for key, value := range values {
		if !c.IsOption(key) {
			return fmt.Errorf("unsupported pricing field: %s", key)
		}
		if key == "billing_setting.billing_mode" {
			if value != "ratio" && value != "tiered_expr" {
				return errors.New("invalid billing mode")
			}
			continue
		}
		if key == "billing_setting.batch_billing_expr" {
			expression, ok := value.(string)
			if !ok || strings.TrimSpace(expression) == "" {
				return errors.New("Batch billing expression is required")
			}
			if c.runtime.Capabilities.Expressions.UsedVars(expression)["header"] {
				return errors.New("Batch billing expressions using header() are not supported")
			}
			if err := c.runtime.ValidateBatchExpression(expression); err != nil {
				return fmt.Errorf("model %s Batch: %w", name, err)
			}
			continue
		}
		if key == "billing_setting.billing_expr" {
			expression, ok := value.(string)
			if !ok || strings.TrimSpace(expression) == "" {
				return errors.New("billing expression is required")
			}
			err := c.runtime.ValidateExpression(name, expression)
			if err != nil {
				return fmt.Errorf("model %s: %w", name, err)
			}
			continue
		}
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return fmt.Errorf("%s must be a finite, non-negative number", key)
		}
	}
	if values["billing_setting.billing_mode"] == "tiered_expr" {
		if _, exists := values["billing_setting.billing_expr"]; !exists {
			if _, builtin := c.runtime.Builtin(name); !builtin {
				return errors.New("billing expression is required")
			}
		}
	}
	return nil
}
func (c *Configuration) DecodeRows(rows []OptionRow) (map[string]map[string]any, map[string]bool, []string, error) {
	values := c.Defaults()
	existing := make(map[string]bool)
	counts := make(map[string]int)
	for _, row := range rows {
		var entries map[string]any
		if err := c.runtime.Capabilities.Codec.UnmarshalJsonStr(row.Value, &entries); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", row.Key, err)
		}
		if entries == nil {
			return nil, nil, nil, fmt.Errorf("%s must be a JSON object", row.Key)
		}
		values[row.Key] = entries
		existing[row.Key] = true
		counts[row.Key]++
	}
	var duplicated []string
	for _, key := range c.runtime.Keys() {
		if counts[key] > 1 {
			duplicated = append(duplicated, key)
		}
	}
	return values, existing, duplicated, nil
}

// OptionTransaction is shared by configuration writes and atomic catalog deletion.
// Implementations retain the native transaction and row locks.
type OptionTransaction interface {
	ReadRows([]string) ([]OptionRow, error)
	Ensure(string, string) error
	Save(string, string) error
	Apply(map[string]map[string]any) error
}
