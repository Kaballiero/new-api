package publication

import (
	"testing"

	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamCatalogPassThroughInvalidatesPricing(t *testing.T) {
	global := model_setting.GetGlobalSettings()
	previous := global.PassThroughRequestEnabled
	t.Cleanup(func() { global.PassThroughRequestEnabled = previous; pricingstate.InvalidatePricingCache() })
	global.PassThroughRequestEnabled = false
	generation := (pricingstate.PricingBuildState{}).Generation()
	require.True(t, handleConfigUpdate("global.pass_through_request_enabled", "true"))
	assert.True(t, global.PassThroughRequestEnabled)
	assert.Greater(t, (pricingstate.PricingBuildState{}).Generation(), generation)
	generation = (pricingstate.PricingBuildState{}).Generation()
	require.True(t, handleConfigUpdate("global.pass_through_request_enabled", "true"))
	assert.Equal(t, generation, (pricingstate.PricingBuildState{}).Generation())
	require.True(t, handleConfigUpdate("global.pass_through_request_enabled", "false"))
	assert.False(t, global.PassThroughRequestEnabled)
	assert.Greater(t, (pricingstate.PricingBuildState{}).Generation(), generation)
}
