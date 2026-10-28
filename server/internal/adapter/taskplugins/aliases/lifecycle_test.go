package aliases_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	aliases "github.com/QuantumNous/new-api/internal/adapter/taskplugins/aliases"
	legacy "github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingInputsNoMemorySyncWithoutDatabaseClearsAliases(t *testing.T) {
	previousDB, previousMemory, previousAliases := legacy.DB, common.MemoryCacheEnabled, aliases.ViewPointerForTest(channelstore.TaskAliases).Load()
	t.Cleanup(func() {
		legacy.DB = previousDB
		common.MemoryCacheEnabled = previousMemory
		aliases.ViewPointerForTest(channelstore.TaskAliases).Store(previousAliases)
	})
	legacy.DB = nil
	common.MemoryCacheEnabled = false
	aliases.ViewPointerForTest(channelstore.TaskAliases).Store(aliases.StaleViewForTest(map[string]aliases.TaskAliasTarget{"stale-alias": {}}))

	require.NotPanics(t, channelstore.InitChannelCache)
	require.NotNil(t, aliases.ViewPointerForTest(channelstore.TaskAliases).Load())
	assert.Empty(t, aliases.FoldedAliasesForTest(aliases.ViewPointerForTest(channelstore.TaskAliases).Load()))
}
