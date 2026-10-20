package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelSettingsSchemaIncludesRelaykitDiscoveryConfiguration(t *testing.T) {
	require.NoError(t, parseModels("../../relaykit/dto"))
	require.Contains(t, modelTypes, "ChannelSettings")
	schema := structToSchema(modelTypes["ChannelSettings"])
	properties := schema["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"type": "string"}, properties["proxy"])
	assert.Equal(t, map[string]interface{}{"type": "string"}, properties["task_plugin_key"])
	require.Contains(t, modelTypes, "ChannelOtherSettings")
	other := structToSchema(modelTypes["ChannelOtherSettings"])["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"$ref": "#/components/schemas/AdvancedCustomConfig"}, other["advanced_custom"])
}

func TestEmbeddedTokenSchemaPreservesFieldsAndAutoGroupOverride(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "token.go"), []byte("package fixture\ntype EmbeddedToken struct { Name string `json:\"name\"`; AutoGroups string `json:\"auto_groups\"` }\ntype TokenEnvelope struct { *EmbeddedToken; AutoGroups []string `json:\"auto_groups\"` }\n"), 0600))
	require.NoError(t, parseModels(dir))
	schema := structToSchema(modelTypes["TokenEnvelope"])
	properties := schema["properties"].(map[string]interface{})
	assert.Equal(t, map[string]interface{}{"type": "string"}, properties["name"])
	assert.Equal(t, map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}, properties["auto_groups"])
}

func TestUserDeletedAtSchemasAllowActiveUserNull(t *testing.T) {
	require.NoError(t, parseModels("../../model"))
	for _, name := range []string{"User", "UserBatchRow"} {
		require.Contains(t, modelTypes, name)
		properties := structToSchema(modelTypes[name])["properties"].(map[string]interface{})
		assert.Equal(t, map[string]interface{}{"type": "string", "format": "date-time", "nullable": true}, properties["DeletedAt"], name)
	}
	data, err := (gorm.DeletedAt{}).MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(data))
}

func TestPricingNamedMapsResolveTransitiveUsageSchemas(t *testing.T) {
	t.Chdir("../..")
	require.NoError(t, bootstrap())
	assert.Equal(t, map[string]any{"$ref": "#/components/schemas/PricingValues"}, identToSchema("PricingValues"))
	schemas := buildSchemas()
	pricing := schemas["PricingValues"].(map[string]any)
	assert.Equal(t, "object", pricing["type"])
	assert.Equal(t, map[string]any{"description": "Any value."}, pricing["additionalProperties"])
	assert.NotContains(t, pricing, "properties")
	entry := schemas["ModelPricingEntry"].(map[string]any)
	assert.Equal(t, []string{"model_name", "version", "configured", "effective"}, entry["required"])
	usage := entry["properties"].(map[string]any)["usage_schema"].(map[string]any)
	assert.Equal(t, map[string]any{"$ref": "#/components/schemas/UsageFieldSchema"}, usage["additionalProperties"])
	assert.NotContains(t, usage, "nullable")
	fields := schemas["UsageFieldSchema"].(map[string]any)
	assert.NotContains(t, fields, "required")
	props := fields["properties"].(map[string]any)
	assert.Equal(t, map[string]any{"$ref": "#/components/schemas/LocalizedText"}, props["description"])
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": map[string]any{"$ref": "#/components/schemas/LocalizedText"}}, props["enumLabels"])
	assert.Equal(t, map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, props["enum"])
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, schemas["LocalizedText"])
	snapshot := schemas["ModelPricingSnapshot"].(map[string]any)
	assert.Equal(t, []string{"entries", "options", "empty_version"}, snapshot["required"])
	change := schemas["ModelPricingChange"].(map[string]any)
	assert.Equal(t, []string{"model_name", "expected_version"}, change["required"])
	assert.Equal(t, map[string]any{"type": "object", "additionalProperties": map[string]any{"description": "Any value."}, "nullable": true}, change["properties"].(map[string]any)["pricing"])
}
