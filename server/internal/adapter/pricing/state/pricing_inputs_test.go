package pricingstate_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pricingInputFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, channel, _ := pricingCatalogOptionFixture(t)
	require.NoError(t, db.AutoMigrate(&channelrecord.Ability{}, &legacy.Model{}, &legacy.Vendor{}))
	require.NoError(t, db.Where("1 = 1").Delete(&channelrecord.Ability{}).Error)
	require.NoError(t, db.Unscoped().Where("1 = 1").Delete(&legacy.Model{}).Error)
	require.NoError(t, db.Unscoped().Where("1 = 1").Delete(&legacy.Vendor{}).Error)
	previousMemory, previousRegistry := common.MemoryCacheEnabled, jsplugin.DefaultRegistry
	common.MemoryCacheEnabled = true
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	originals := map[string]string{}
	for _, key := range pricingstate.ModelPricingOptionKeys[:8] {
		originals[key] = pricingstate.LegacyPricingOptionValue(key)
	}
	t.Cleanup(func() {
		for key, value := range originals {
			require.NoError(t, publication.PublishOption(key, value))
		}
		common.MemoryCacheEnabled = previousMemory
		jsplugin.DefaultRegistry = previousRegistry
		pricingstate.InvalidatePricingCache()
	})
	channel.Type = constant.ChannelTypeOpenAI
	channel.Models = "cache-model"
	channel.Group = "default"
	require.NoError(t, db.Save(channel).Error)
	require.NoError(t, db.Create(&channelrecord.Ability{ChannelId: channel.Id, Model: "cache-model", Group: "default", Enabled: true}).Error)
	require.NoError(t, db.Create(&legacy.Vendor{Id: 1700, Name: "Initial creator", Status: 1}).Error)
	require.NoError(t, db.Create(&legacy.Model{ModelName: "cache-model", Description: "initial", VendorID: 1700, Status: 1}).Error)
	require.NoError(t, publication.PublishOption("ModelPrice", `{"cache-model":1}`))
	channelstore.InitChannelCache()
	pricingruntime.RefreshPricing()
	require.NotNil(t, (*pricingstate.TestPublishedPricing).Load())
	return db
}

func TestPricingInputsAgeAndUnchangedPollDoNotRebuild(t *testing.T) {
	db := pricingInputFixture(t)
	aged := *(*pricingstate.TestPublishedPricing).Load()
	(*pricingstate.BuiltAtForTest(&aged)) = time.Now().Add(-24 * time.Hour)
	(*pricingstate.TestPublishedPricing).Store(&aged)
	(*pricingstate.TestLastGetPricingTime) = (*pricingstate.BuiltAtForTest(&aged))
	var reads atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:pricing_reads", func(*gorm.DB) { reads.Add(1) }))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:pricing_rows", func(*gorm.DB) { reads.Add(1) }))
	t.Cleanup(func() {
		db.Callback().Query().Remove("test:pricing_reads")
		db.Callback().Row().Remove("test:pricing_rows")
	})
	for range 5 {
		_, _, _, err := pricingruntime.GetPricingCatalogueSnapshot()
		require.NoError(t, err)
		require.Len(t, pricingruntime.GetPricing(), 1)
		require.NotEmpty(t, pricingruntime.GetVendors())
	}
	assert.Zero(t, reads.Load(), "age alone performs no projection or database reads")
	assert.Same(t, &aged, (*pricingstate.TestPublishedPricing).Load())
	generation := (*pricingstate.TestPricingInputsGeneration).Load()
	pricingruntime.SyncPricingCatalogueInputs()
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load(), "unchanged source poll does not dirty the projection")
	require.NoError(t, db.Model(&legacy.Model{}).Where("model_name = ?", "cache-model").Update("updated_time", 123456).Error)
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", 970).Updates(map[string]any{"used_quota": 99, "test_time": 123, "priority": 9}).Error)
	pricingruntime.SyncPricingCatalogueInputs()
	channelstore.InitChannelCache()
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load(), "timestamps, quota and priority do not affect projection inputs")
	channel, err := channelstore.GetChannelById(970, true)
	require.NoError(t, err)
	channel.UsedQuota++
	cached, err := channelstore.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	cached.UsedQuota = channel.UsedQuota
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load(), "quota-only cache updates are no-ops for pricing")
	reads.Store(0)
	pricingruntime.GetPricing()
	_, _, _, err = pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	assert.Zero(t, reads.Load())
	assert.Same(t, &aged, (*pricingstate.TestPublishedPricing).Load())
}

func TestPricingInputsPollObservesMetadataVendorsAndRoutes(t *testing.T) {
	db := pricingInputFixture(t)
	generation := (*pricingstate.TestPricingInputsGeneration).Load()
	require.NoError(t, db.Model(&legacy.Model{}).Where("model_name = ?", "cache-model").Update("description", "changed on another node").Error)
	pricingruntime.SyncPricingCatalogueInputs()
	require.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
	generation = (*pricingstate.TestPricingInputsGeneration).Load()
	pricingruntime.SyncPricingCatalogueInputs()
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load())
	rows := pricingruntime.GetPricing()
	require.Len(t, rows, 1)
	assert.Equal(t, "changed on another node", rows[0].Description)
	require.NoError(t, db.Model(&legacy.Vendor{}).Where("id = ?", 1700).Update("name", "New creator").Error)
	pricingruntime.SyncPricingCatalogueInputs()
	require.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
	pricingruntime.GetPricing()
	assert.Contains(t, pricingruntime.GetVendors(), pricingdomain.PricingVendor{ID: 1700, Name: "New creator"})
	generation = (*pricingstate.TestPricingInputsGeneration).Load()
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", 970).Update("model_mapping", `{"cache-model":"gpt-image-1"}`).Error)
	pricingruntime.SyncPricingCatalogueInputs()
	require.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
	rows = pricingruntime.GetPricing()
	require.Len(t, rows, 1)
	offers := rows[0].CatalogOffersForGroup("default")
	require.NotEmpty(t, offers)
	assert.Equal(t, "image_generation", offers[0].Operation)
	// Disabled-channel enabled abilities still contribute inferred inventory.
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", 970).Update("status", common.ChannelStatusManuallyDisabled).Error)
	pricingruntime.SyncPricingCatalogueInputs()
	pricingruntime.GetPricing()
	generation = (*pricingstate.TestPricingInputsGeneration).Load()
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", 970).Update("type", constant.ChannelTypeAnthropic).Error)
	pricingruntime.SyncPricingCatalogueInputs()
	assert.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
}

func TestPricingInputsLegacyOptionsAndAbilityStatusInvalidateOnlyChanges(t *testing.T) {
	pricingInputFixture(t)
	for _, key := range pricingstate.ModelPricingOptionKeys[:8] {
		require.NoError(t, publication.PublishOption(key, `{"cache-model":2,"neighbor":1}`))
		generation := (*pricingstate.TestPricingInputsGeneration).Load()
		require.NoError(t, publication.PublishOption(key, `{ "neighbor": 1, "cache-model": 2 }`))
		assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load(), key+" equivalent JSON must be a no-op")
		require.NoError(t, publication.PublishOption(key, `{"cache-model":3,"neighbor":1}`))
		assert.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation, key)
	}
	pricingruntime.GetPricing()
	generation := (*pricingstate.TestPricingInputsGeneration).Load()
	require.NoError(t, channelstore.UpdateAbilityStatus(970, true))
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load())
	require.NoError(t, channelstore.UpdateAbilityStatus(970, false))
	assert.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
	assert.Empty(t, pricingruntime.GetPricing())
	generation = (*pricingstate.TestPricingInputsGeneration).Load()
	require.NoError(t, channelstore.UpdateAbilityStatus(970, false))
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load())
	require.NoError(t, channelstore.UpdateAbilityStatus(970, true))
	assert.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
	require.Len(t, pricingruntime.GetPricing(), 1)
	// Controller validation may apply the map before option persistence.
	generation = (*pricingstate.TestPricingInputsGeneration).Load()
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(`{"cache-model":7}`))
	require.NoError(t, publication.PublishOption("ImageRatio", `{"cache-model":7}`))
	assert.Greater(t, (*pricingstate.TestPricingInputsGeneration).Load(), generation)
}

func TestPricingInputsFailedGenerationHasBoundedRetry(t *testing.T) {
	db := pricingInputFixture(t)
	good := (*pricingstate.TestPublishedPricing).Load()
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", 970).Update("setting", "{").Error)
	pricingstate.InvalidatePricingCache()
	// Simulate a failed attempt whose original deadline expired while its SQL
	// was in flight; completion must start a fresh bounded retry window.
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:expired_attempt", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			(*pricingstate.TestPricingRetryAfter).Store(time.Now().Add(-time.Second).UnixNano())
		}
	}))
	pricingruntime.RefreshPricing()
	completed := time.Now()
	require.NoError(t, db.Callback().Query().Remove("test:expired_attempt"))
	assert.Greater(t, (*pricingstate.TestPricingRetryAfter).Load(), completed.Add(pricingstate.TestPricingBuildRetryDelay-time.Second).UnixNano())
	assert.Same(t, good, (*pricingstate.TestPublishedPricing).Load())
	var attempts atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:failed_generation", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			attempts.Add(1)
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove("test:failed_generation") })
	for range 20 {
		_, _, _, err := pricingruntime.GetPricingCatalogueSnapshot()
		require.NoError(t, err)
		pricingruntime.GetPricing()
	}
	assert.Zero(t, attempts.Load(), "unchanged failed inputs must not rebuild on every request")
	pricingstate.InvalidatePricingCache()
	_, _, _, err := pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !(*pricingstate.TestPricingRefreshRunning).Load() }, time.Second, time.Millisecond)
	assert.EqualValues(t, 1, attempts.Load(), "new input generation retries immediately")
	for range 20 {
		_, _, _, err := pricingruntime.GetPricingCatalogueSnapshot()
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, attempts.Load())
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", 970).Update("setting", "{}").Error)
	pricingruntime.SyncPricingCatalogueInputs()
	pricingruntime.GetPricing()
	assert.NotSame(t, good, (*pricingstate.TestPublishedPricing).Load())
	assert.Equal(t, (*pricingstate.TestPricingInputsGeneration).Load(), (*pricingstate.TestPublishedPricing).Load().Generation())
}

func TestPricingInputsRegistryGenerationFencesQueuedBuild(t *testing.T) {
	db := pricingInputFixture(t)
	source := pricingUsagePluginSource("1.0.0", `{"seconds":{"type":"number","unit":"second"}}`)
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	pricingruntime.GetPricing()
	good := (*pricingstate.TestPublishedPricing).Load()
	generation := (*pricingstate.TestPricingInputsGeneration).Load()
	entered, release := make(chan struct{}), make(chan struct{})
	releaseBuild := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseBuild)
	var once sync.Once
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:registry_build", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Row().Remove("test:registry_build") })
	_, err = jsplugin.DefaultRegistry.Register(strings.Replace(source, "1.0.0", "1.1.0", 1), jsplugin.Options{})
	require.NoError(t, err)
	_, _, _, err = pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("registry-triggered build did not reach source barrier")
	}
	_, err = jsplugin.DefaultRegistry.Register(strings.Replace(source, "1.0.0", "1.2.0", 1), jsplugin.Options{})
	require.NoError(t, err)
	releaseBuild()
	require.Eventually(t, func() bool { return !(*pricingstate.TestPricingRefreshRunning).Load() }, time.Second, time.Millisecond)
	assert.Same(t, good, (*pricingstate.TestPublishedPricing).Load(), "captured older routing generation cannot publish over newer plugins")
	assert.Equal(t, generation, (*pricingstate.TestPricingInputsGeneration).Load(), "registry freshness does not depend on an unrelated invalidation")
	pricingruntime.GetPricing()
	assert.Equal(t, jsplugin.DefaultRegistry.Generation().Number, (*pricingstate.TestPublishedPricing).Load().PluginGeneration())
}

func TestPricingInputsQueuedBackgroundCoalescesWithAdminBuild(t *testing.T) {
	db := pricingInputFixture(t)
	var builds atomic.Int64
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:coalesced_pricing_build", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			builds.Add(1)
		}
	}))
	t.Cleanup(func() { db.Callback().Row().Remove("test:coalesced_pricing_build") })
	pricingstate.InvalidatePricingCache()
	(*pricingstate.TestUpdatePricingLock).Lock()
	_, _, _, err := pricingruntime.GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	(*pricingstate.TestModelSupportEndpointsLock).Lock()
	pricingruntime.RebuildPricingCatalogueLocked()
	(*pricingstate.TestModelSupportEndpointsLock).Unlock()
	(*pricingstate.TestUpdatePricingLock).Unlock()
	require.Eventually(t, func() bool { return !(*pricingstate.TestPricingRefreshRunning).Load() }, time.Second, time.Millisecond)
	assert.EqualValues(t, 1, builds.Load(), "queued background work observes the already-published admin build")
}

func TestPricingInputsNoMemorySyncNeverRepairsUnrelatedSettings(t *testing.T) {
	db := pricingInputFixture(t)
	common.MemoryCacheEnabled = false
	require.NoError(
		t,
		db.Create(&legacy.Channel{
			Id:            971,
			Name:          "unrelated",
			Type:          constant.ChannelTypeAdvancedCustom,
			Status:        common.ChannelStatusEnabled,
			OtherSettings: "{",
		}).Error,
	)
	loaded, release := make(chan struct{}), make(chan struct{})
	releaseSync := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseSync)
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:no_memory_source", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			once.Do(func() { close(loaded); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove("test:no_memory_source") })
	done := make(chan struct{})
	go func() { channelstore.InitChannelCache(); close(done) }()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("channel sync did not reach source barrier")
	}
	require.NoError(
		t,
		db.Model(&legacy.Channel{}).Where(
			"id = ?",
			971,
		).Updates(map[string]any{
			"name":       "concurrent-name",
			"key":        "concurrent-key",
			"used_quota": 77,
		}).Error,
	)
	var writes atomic.Int64
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:no_memory_writes", func(*gorm.DB) { writes.Add(1) }))
	t.Cleanup(func() { db.Callback().Update().Remove("test:no_memory_writes") })
	releaseSync()
	<-done
	assert.Zero(t, writes.Load(), "no-memory pricing sync must not repair channel configuration")
	var channel legacy.Channel
	require.NoError(t, db.First(&channel, 971).Error)
	assert.Equal(t, "{", channel.OtherSettings)
	assert.Equal(t, "concurrent-name", channel.Name)
	assert.Equal(t, "concurrent-key", channel.Key)
	assert.Equal(t, int64(77), channel.UsedQuota)
}
