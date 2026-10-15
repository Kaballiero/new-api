package model

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pricingInputFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, channel, _ := pricingCatalogOptionFixture(t)
	require.NoError(t, db.AutoMigrate(&Ability{}, &Model{}, &Vendor{}))
	require.NoError(t, db.Where("1 = 1").Delete(&Ability{}).Error)
	require.NoError(t, db.Unscoped().Where("1 = 1").Delete(&Model{}).Error)
	require.NoError(t, db.Unscoped().Where("1 = 1").Delete(&Vendor{}).Error)
	previousMemory, previousRegistry := common.MemoryCacheEnabled, jsplugin.DefaultRegistry
	common.MemoryCacheEnabled = true
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	originals := map[string]string{}
	for _, key := range modelPricingOptionKeys[:8] {
		originals[key] = legacyPricingOptionValue(key)
	}
	t.Cleanup(func() {
		for key, value := range originals {
			require.NoError(t, updateOptionMap(key, value))
		}
		common.MemoryCacheEnabled = previousMemory
		jsplugin.DefaultRegistry = previousRegistry
		InvalidatePricingCache()
	})
	channel.Type = constant.ChannelTypeOpenAI
	channel.Models = "cache-model"
	channel.Group = "default"
	require.NoError(t, db.Save(channel).Error)
	require.NoError(t, db.Create(&Ability{ChannelId: channel.Id, Model: "cache-model", Group: "default", Enabled: true}).Error)
	require.NoError(t, db.Create(&Vendor{Id: 1700, Name: "Initial creator", Status: 1}).Error)
	require.NoError(t, db.Create(&Model{ModelName: "cache-model", Description: "initial", VendorID: 1700, Status: 1}).Error)
	require.NoError(t, updateOptionMap("ModelPrice", `{"cache-model":1}`))
	InitChannelCache()
	RefreshPricing()
	require.NotNil(t, publishedPricing.Load())
	return db
}

func TestPricingInputsAgeAndUnchangedPollDoNotRebuild(t *testing.T) {
	db := pricingInputFixture(t)
	aged := *publishedPricing.Load()
	aged.builtAt = time.Now().Add(-24 * time.Hour)
	publishedPricing.Store(&aged)
	lastGetPricingTime = aged.builtAt
	var reads atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:pricing_reads", func(*gorm.DB) { reads.Add(1) }))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:pricing_rows", func(*gorm.DB) { reads.Add(1) }))
	t.Cleanup(func() {
		db.Callback().Query().Remove("test:pricing_reads")
		db.Callback().Row().Remove("test:pricing_rows")
	})
	for range 5 {
		_, _, _, err := GetPricingCatalogueSnapshot()
		require.NoError(t, err)
		require.Len(t, GetPricing(), 1)
		require.NotEmpty(t, GetVendors())
	}
	assert.Zero(t, reads.Load(), "age alone performs no projection or database reads")
	assert.Same(t, &aged, publishedPricing.Load())
	generation := pricingInputsGeneration.Load()
	syncPricingCatalogueInputs()
	assert.Equal(t, generation, pricingInputsGeneration.Load(), "unchanged source poll does not dirty the projection")
	require.NoError(t, db.Model(&Model{}).Where("model_name = ?", "cache-model").Update("updated_time", 123456).Error)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 970).Updates(map[string]any{"used_quota": 99, "test_time": 123, "priority": 9}).Error)
	syncPricingCatalogueInputs()
	InitChannelCache()
	assert.Equal(t, generation, pricingInputsGeneration.Load(), "timestamps, quota and priority do not affect projection inputs")
	channel, err := GetChannelById(970, true)
	require.NoError(t, err)
	channel.UsedQuota++
	CacheUpdateChannel(channel)
	assert.Equal(t, generation, pricingInputsGeneration.Load(), "quota-only cache updates are no-ops for pricing")
	reads.Store(0)
	GetPricing()
	_, _, _, err = GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	assert.Zero(t, reads.Load())
	assert.Same(t, &aged, publishedPricing.Load())
}

func TestPricingInputsPollObservesMetadataVendorsAndRoutes(t *testing.T) {
	db := pricingInputFixture(t)
	generation := pricingInputsGeneration.Load()
	require.NoError(t, db.Model(&Model{}).Where("model_name = ?", "cache-model").Update("description", "changed on another node").Error)
	syncPricingCatalogueInputs()
	require.Greater(t, pricingInputsGeneration.Load(), generation)
	generation = pricingInputsGeneration.Load()
	syncPricingCatalogueInputs()
	assert.Equal(t, generation, pricingInputsGeneration.Load())
	rows := GetPricing()
	require.Len(t, rows, 1)
	assert.Equal(t, "changed on another node", rows[0].Description)
	require.NoError(t, db.Model(&Vendor{}).Where("id = ?", 1700).Update("name", "New creator").Error)
	syncPricingCatalogueInputs()
	require.Greater(t, pricingInputsGeneration.Load(), generation)
	GetPricing()
	assert.Contains(t, GetVendors(), PricingVendor{ID: 1700, Name: "New creator"})
	generation = pricingInputsGeneration.Load()
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 970).Update("model_mapping", `{"cache-model":"gpt-image-1"}`).Error)
	syncPricingCatalogueInputs()
	require.Greater(t, pricingInputsGeneration.Load(), generation)
	rows = GetPricing()
	require.Len(t, rows, 1)
	offers := rows[0].CatalogOffersForGroup("default")
	require.NotEmpty(t, offers)
	assert.Equal(t, "image_generation", offers[0].Operation)
	// Disabled-channel enabled abilities still contribute inferred inventory.
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 970).Update("status", common.ChannelStatusManuallyDisabled).Error)
	syncPricingCatalogueInputs()
	GetPricing()
	generation = pricingInputsGeneration.Load()
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 970).Update("type", constant.ChannelTypeAnthropic).Error)
	syncPricingCatalogueInputs()
	assert.Greater(t, pricingInputsGeneration.Load(), generation)
}

func TestPricingInputsLegacyOptionsAndAbilityStatusInvalidateOnlyChanges(t *testing.T) {
	pricingInputFixture(t)
	for _, key := range modelPricingOptionKeys[:8] {
		require.NoError(t, updateOptionMap(key, `{"cache-model":2,"neighbor":1}`))
		generation := pricingInputsGeneration.Load()
		require.NoError(t, updateOptionMap(key, `{ "neighbor": 1, "cache-model": 2 }`))
		assert.Equal(t, generation, pricingInputsGeneration.Load(), key+" equivalent JSON must be a no-op")
		require.NoError(t, updateOptionMap(key, `{"cache-model":3,"neighbor":1}`))
		assert.Greater(t, pricingInputsGeneration.Load(), generation, key)
	}
	GetPricing()
	generation := pricingInputsGeneration.Load()
	require.NoError(t, UpdateAbilityStatus(970, true))
	assert.Equal(t, generation, pricingInputsGeneration.Load())
	require.NoError(t, UpdateAbilityStatus(970, false))
	assert.Greater(t, pricingInputsGeneration.Load(), generation)
	assert.Empty(t, GetPricing())
	generation = pricingInputsGeneration.Load()
	require.NoError(t, UpdateAbilityStatus(970, false))
	assert.Equal(t, generation, pricingInputsGeneration.Load())
	require.NoError(t, UpdateAbilityStatus(970, true))
	assert.Greater(t, pricingInputsGeneration.Load(), generation)
	require.Len(t, GetPricing(), 1)
	// Controller validation may apply the map before option persistence.
	generation = pricingInputsGeneration.Load()
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(`{"cache-model":7}`))
	require.NoError(t, updateOptionMap("ImageRatio", `{"cache-model":7}`))
	assert.Greater(t, pricingInputsGeneration.Load(), generation)
}

func TestPricingInputsFailedGenerationHasBoundedRetry(t *testing.T) {
	db := pricingInputFixture(t)
	good := publishedPricing.Load()
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 970).Update("setting", "{").Error)
	InvalidatePricingCache()
	// Simulate a failed attempt whose original deadline expired while its SQL
	// was in flight; completion must start a fresh bounded retry window.
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:expired_attempt", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			pricingRetryAfter.Store(time.Now().Add(-time.Second).UnixNano())
		}
	}))
	RefreshPricing()
	completed := time.Now()
	require.NoError(t, db.Callback().Query().Remove("test:expired_attempt"))
	assert.Greater(t, pricingRetryAfter.Load(), completed.Add(pricingBuildRetryDelay-time.Second).UnixNano())
	assert.Same(t, good, publishedPricing.Load())
	var attempts atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:failed_generation", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			attempts.Add(1)
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove("test:failed_generation") })
	for range 20 {
		_, _, _, err := GetPricingCatalogueSnapshot()
		require.NoError(t, err)
		GetPricing()
	}
	assert.Zero(t, attempts.Load(), "unchanged failed inputs must not rebuild on every request")
	InvalidatePricingCache()
	_, _, _, err := GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !pricingRefreshRunning.Load() }, time.Second, time.Millisecond)
	assert.EqualValues(t, 1, attempts.Load(), "new input generation retries immediately")
	for range 20 {
		_, _, _, err := GetPricingCatalogueSnapshot()
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, attempts.Load())
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 970).Update("setting", "{}").Error)
	syncPricingCatalogueInputs()
	GetPricing()
	assert.NotSame(t, good, publishedPricing.Load())
	assert.Equal(t, pricingInputsGeneration.Load(), publishedPricing.Load().generation)
}

func TestPricingInputsRegistryGenerationFencesQueuedBuild(t *testing.T) {
	db := pricingInputFixture(t)
	source := pricingUsagePluginSource("1.0.0", `{"seconds":{"type":"number","unit":"second"}}`)
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	GetPricing()
	good := publishedPricing.Load()
	generation := pricingInputsGeneration.Load()
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
	_, _, _, err = GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("registry-triggered build did not reach source barrier")
	}
	_, err = jsplugin.DefaultRegistry.Register(strings.Replace(source, "1.0.0", "1.2.0", 1), jsplugin.Options{})
	require.NoError(t, err)
	releaseBuild()
	require.Eventually(t, func() bool { return !pricingRefreshRunning.Load() }, time.Second, time.Millisecond)
	assert.Same(t, good, publishedPricing.Load(), "captured older routing generation cannot publish over newer plugins")
	assert.Equal(t, generation, pricingInputsGeneration.Load(), "registry freshness does not depend on an unrelated invalidation")
	GetPricing()
	assert.Equal(t, jsplugin.DefaultRegistry.Generation().Number, publishedPricing.Load().pluginGeneration)
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
	InvalidatePricingCache()
	updatePricingLock.Lock()
	_, _, _, err := GetPricingCatalogueSnapshot()
	require.NoError(t, err)
	modelSupportEndpointsLock.Lock()
	rebuildPricingCatalogueLocked()
	modelSupportEndpointsLock.Unlock()
	updatePricingLock.Unlock()
	require.Eventually(t, func() bool { return !pricingRefreshRunning.Load() }, time.Second, time.Millisecond)
	assert.EqualValues(t, 1, builds.Load(), "queued background work observes the already-published admin build")
}

func TestPricingInputsNoMemorySyncNeverRepairsUnrelatedSettings(t *testing.T) {
	db := pricingInputFixture(t)
	common.MemoryCacheEnabled = false
	require.NoError(t, db.Create(&Channel{Id: 971, Name: "unrelated", Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled, OtherSettings: "{"}).Error)
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
	go func() { InitChannelCache(); close(done) }()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("channel sync did not reach source barrier")
	}
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 971).Updates(map[string]any{"name": "concurrent-name", "key": "concurrent-key", "used_quota": 77}).Error)
	var writes atomic.Int64
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:no_memory_writes", func(*gorm.DB) { writes.Add(1) }))
	t.Cleanup(func() { db.Callback().Update().Remove("test:no_memory_writes") })
	releaseSync()
	<-done
	assert.Zero(t, writes.Load(), "no-memory pricing sync must not repair channel configuration")
	var channel Channel
	require.NoError(t, db.First(&channel, 971).Error)
	assert.Equal(t, "{", channel.OtherSettings)
	assert.Equal(t, "concurrent-name", channel.Name)
	assert.Equal(t, "concurrent-key", channel.Key)
	assert.Equal(t, int64(77), channel.UsedQuota)
}
