package model_test

import (
	"context"
	"errors"
	"github.com/glebarez/sqlite"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPricingCatalogOptionsPersistReloadAndRevision(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	before := model.GetPricingRevision()
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	require.Greater(t, model.GetPricingRevision(), before)
	saved := model.RequireOptionValue(t, db, model.PricingCatalogOptionKey)
	assert.NotContains(t, saved, channel.Key)
	assert.False(t, model.IsModelPricingOption(model.PricingCatalogOptionKey))
	published, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, entry, published)
	revision := model.GetPricingRevision()
	_ = settingsapp.Default().Reload.Execute()
	assert.Equal(t, revision, model.GetPricingRevision())
	model.PricingCatalogState.Store(nil)
	_ = settingsapp.Default().Reload.Execute()
	reloaded, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, entry, reloaded)
	revision = model.GetPricingRevision()
	entry.FetchedAt = "2026-10-13T14:41:19Z"
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	assert.Equal(t, revision, model.GetPricingRevision(), "timestamp-only metadata refresh preserves projection")
}

func TestPricingCatalogSourceSurvivesUnrelatedRoutes(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	mapping := `{"neighbor":"other/upstream","alias":"upstream/model"}`
	changed := *channel
	changed.Models = "neighbor,alias"
	changed.ModelMapping = &mapping
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"models": changed.Models, "model_mapping": mapping,
	}).Error)
	_, known := model.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known, "another route must not invalidate this upstream price")
	require.NoError(t, model.PublishPricingCatalogEntry(entry), "unrelated route changes must not reject in-flight publication")

	remapped := `{"alias":"different/upstream"}`
	for _, change := range []struct {
		name    string
		channel model.Channel
	}{
		{"model removed", model.Channel{Models: "neighbor", ModelMapping: &mapping}},
		{"alias remapped", model.Channel{Models: "alias", ModelMapping: &remapped}},
	} {
		t.Run(change.name, func(t *testing.T) {
			replaced := changed
			replaced.Models = change.channel.Models
			replaced.ModelMapping = change.channel.ModelMapping
			_, known := model.GetPricingCatalogEntry(&replaced, entry.UpstreamModel, entry.Endpoint)
			assert.False(t, known)
		})
	}
	changed.Key = "rotated-fixture"
	_, known = model.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known, "credential changes must invalidate source prices")
}

func TestPricingCatalogLegacyFingerprintCompatibility(t *testing.T) {
	_, channel, entry := model.PricingCatalogOptionFixture(t)
	entry.Fingerprint = model.PricingCatalogFingerprint(channel, entry.UpstreamModel, entry.Endpoint, []any{channel.Models, channel.GetModelMapping()})
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	_, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known)
	changed := *channel
	changed.Models += ",neighbor"
	_, known = model.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known, "stale legacy fingerprints require fresh metadata")
}

func TestPricingCatalogOptionsRejectInvalidAndRetainSiblings(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	sibling := entry
	sibling.UpstreamModel = "upstream/other"
	sibling.Fingerprint = model.PricingCatalogSourceFingerprint(channel, sibling.UpstreamModel, sibling.Endpoint)
	require.NoError(t, model.PublishPricingCatalogEntry(sibling))
	saved := model.RequireOptionValue(t, db, model.PricingCatalogOptionKey)
	revision := model.GetPricingRevision()
	invalid := strings.Replace(saved, `"amount_usd":0.03`, `"amount_usd":-1`, 1)
	require.Error(t, settingsapp.Default().Write.Execute(model.PricingCatalogOptionKey, invalid))
	assert.Equal(t, saved, model.RequireOptionValue(t, db, model.PricingCatalogOptionKey))
	assert.Equal(t, revision, model.GetPricingRevision())
	_, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known)
	_, known = model.GetPricingCatalogEntry(channel, sibling.UpstreamModel, sibling.Endpoint)
	assert.True(t, known)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", "changed-model").Error)
	require.ErrorContains(t, model.PublishPricingCatalogEntry(entry), "source changed")
	assert.Equal(t, saved, model.RequireOptionValue(t, db, model.PricingCatalogOptionKey))
	changed := *channel
	changed.Models = "changed-model"
	_, known = model.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}

func TestPricingCatalogOptionsTransactionFailureDoesNotPublish(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	saved := model.RequireOptionValue(t, db, model.PricingCatalogOptionKey)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:pricing_catalog_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(errors.New("pricing catalog transaction failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Update().Remove("test:pricing_catalog_fail") })
	revision := model.GetPricingRevision()
	entry.FetchedAt = "2026-10-13T15:41:19Z"
	require.Error(t, model.PublishPricingCatalogEntry(entry))
	assert.Equal(t, saved, model.RequireOptionValue(t, db, model.PricingCatalogOptionKey))
	assert.Equal(t, revision, model.GetPricingRevision())
	retained, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, "2026-10-13T13:41:19Z", retained.FetchedAt)
}

func TestPricingCatalogOptionsCancelledPublicationRetainsLastValid(t *testing.T) {
	db, _, entry := model.PricingCatalogOptionFixture(t)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	previous := model.RequireOptionValue(t, db, model.PricingCatalogOptionKey)
	revision := model.GetPricingRevision()
	ctx, cancel := context.WithCancel(context.Background())
	model.GroupRatioOptionMutex.Lock()
	done := make(chan error, 1)
	go func() { done <- model.PublishPricingCatalogEntryContext(ctx, entry) }()
	cancel()
	model.GroupRatioOptionMutex.Unlock()
	require.ErrorIs(t, <-done, context.Canceled)
	assert.Equal(t, previous, model.RequireOptionValue(t, db, model.PricingCatalogOptionKey))
	assert.Equal(t, revision, model.GetPricingRevision())
}

func TestPricingCatalogOptionsInvalidPersistedStateRetainsLastValid(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	valid := model.RequireOptionValue(t, db, model.PricingCatalogOptionKey)
	revision := model.GetPricingRevision()
	for _, invalid := range []string{`{`, `{"version":0,"entries":{}}`, strings.Repeat("x", model.PricingCatalogMaxBytes+1), strings.Replace(valid, entry.FetchedAt, "invalid", 1)} {
		require.NoError(t, db.Model(&model.Option{}).Where(map[string]any{"key": model.PricingCatalogOptionKey}).Update("value", invalid).Error)
		_ = settingsapp.Default().Reload.Execute()
		retained, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
		require.True(t, known)
		assert.Equal(t, entry, retained)
		assert.Equal(t, revision, model.GetPricingRevision())
		assert.Equal(t, valid, common.OptionMap[model.PricingCatalogOptionKey])
	}
}

func TestPricingCatalogOptionsConcurrentMergeSyncPreservesBillingOptions(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	sentinel := model.Option{Key: "ModelRatio", Value: `{"financial-sentinel":7.125}`}
	require.NoError(t, db.Create(&sentinel).Error)
	previousRatio := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatio)) })
	second := entry
	second.UpstreamModel = "upstream/sibling"
	second.Fingerprint = model.PricingCatalogSourceFingerprint(channel, second.UpstreamModel, second.Endpoint)
	var workers sync.WaitGroup
	errors := make(chan error, 2)
	model.GroupRatioOptionMutex.Lock()
	workers.Go(func() { errors <- model.PublishPricingCatalogEntry(entry) })
	workers.Go(func() { errors <- model.PublishPricingCatalogEntry(second) })
	workers.Go(func() { _ = settingsapp.Default().Reload.Execute() })
	model.GroupRatioOptionMutex.Unlock()
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	_, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	_, known = model.GetPricingCatalogEntry(channel, second.UpstreamModel, second.Endpoint)
	require.True(t, known)
	assert.Equal(t, sentinel.Value, model.RequireOptionValue(t, db, sentinel.Key))
}

func TestImagePricingTokenInputsStayWithTheirProvider(t *testing.T) {
	amount := func(v float64) *float64 { return &v }
	image := model.CatalogOffer{ID: "43:google/model:studio:0:default", Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []model.CatalogPrice{{Component: "image_output", Unit: "million_tokens", AmountUSD: amount(60)}}}
	tokens := model.CatalogOffer{ID: "43:google/model:studio:0:0", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount(.5)}, {Component: "output", Unit: "million_tokens", AmountUSD: amount(3)}, {Component: "cache_read", Unit: "million_tokens", AmountUSD: amount(0)}}}
	for _, tc := range []struct {
		name, id  string
		wantInput bool
	}{
		{"same provider", tokens.ID, true},
		{"different provider", "43:google/model:vertex:0:0", false},
		{"different channel", "44:google/model:studio:0:0", false},
		{"missing provider identity", "unknown", false},
		{"empty provider identity", "43:google/model::0:0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tokens
			source.ID = tc.id
			offers := []model.CatalogOffer{image, source}
			before, err := common.Marshal(offers)
			require.NoError(t, err)
			result := model.ImagePricingWithTokenInputs(offers)
			selected := model.SelectCatalogOffer(result[:1])
			require.NotNil(t, selected)
			prices := map[string]model.CatalogPrice{}
			for _, p := range selected.Prices {
				prices[p.Component] = p
			}
			assert.Equal(t, 60., *prices["image_output"].AmountUSD)
			assert.NotContains(t, prices, "output")
			if tc.wantInput {
				require.Contains(t, prices, "input")
				assert.Equal(t, .5, *prices["input"].AmountUSD)
				require.Contains(t, prices, "cache_read")
				assert.Equal(t, 0., *prices["cache_read"].AmountUSD)
			} else {
				assert.NotContains(t, prices, "input")
			}
			after, err := common.Marshal(offers)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after))
		})
	}
	t.Run("both provider tags absent", func(t *testing.T) {
		native := image
		native.ID = "43:google/model::0:"
		source := tokens
		source.ID = "43:google/model::0:0"
		result := model.ImagePricingWithTokenInputs([]model.CatalogOffer{native, source})
		assert.Len(t, result[0].Prices, 1)
	})

	t.Run("native input wins", func(t *testing.T) {
		native := image
		native.Prices = append(slices.Clone(image.Prices), model.CatalogPrice{Component: "input", Unit: "million_tokens", AmountUSD: amount(.7)})
		result := model.ImagePricingWithTokenInputs([]model.CatalogOffer{native, tokens})
		for _, p := range result[0].Prices {
			if p.Component == "input" {
				assert.Equal(t, .7, *p.AmountUSD)
			}
		}
	})
	t.Run("conditional alternatives retained", func(t *testing.T) {
		conditional := tokens
		conditional.ID = "43:google/model:studio:0:1"
		conditional.PublicConditional = true
		conditional.PublicConditions = []billingexpr.TierCondition{{Var: "len", Op: ">", Value: 100}}
		conditional.Prices = []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount(1)}}
		source := tokens
		source.PublicAlternatives = []model.CatalogOffer{tokens, conditional}
		result := model.ImagePricingWithTokenInputs([]model.CatalogOffer{image, source})
		require.Len(t, result[0].PublicAlternatives, 2)
		assert.Equal(t, conditional.PublicConditions, result[0].PublicAlternatives[1].PublicConditions)
		assert.True(t, result[0].PublicAlternatives[1].PublicConditional)
	})
}

func TestPricingCatalogPublicationLocksSourceUntilCommit(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "catalog.db") + "?_pragma=busy_timeout(100)&_pragma=journal_mode(WAL)&_pragma=synchronous(OFF)"
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	writer, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousState, previousOptions := model.DB, model.PricingCatalogState.Load(), common.OptionMap
	model.DB = db
	model.PricingCatalogState.Store(nil)
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		model.DB = previousDB
		model.PricingCatalogState.Store(previousState)
		common.OptionMap = previousOptions
		for _, handle := range []*gorm.DB{db, writer} {
			sqlDB, err := handle.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Option{}))
	channel := &model.Channel{Id: 980, Type: constant.ChannelTypeOpenRouter, Status: common.ChannelStatusEnabled, Key: "fixture", Models: "upstream/model", Group: "default"}
	require.NoError(t, db.Create(channel).Error)
	amount := .01
	entry := model.PricingCatalogEntry{ChannelID: channel.Id, UpstreamModel: "upstream/model", Endpoint: constant.EndpointTypeOpenAI, Source: "https://openrouter.ai/api/v1/models/upstream/model/endpoints", FetchedAt: "2026-10-15T00:00:00Z", Fingerprint: model.PricingCatalogSourceFingerprint(channel, "upstream/model", constant.EndpointTypeOpenAI), Offers: []model.CatalogOffer{{ID: "native", Operation: "text", Mode: "sync", MainComponent: "input", Prices: []model.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: &amount}}}}}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:source_locked", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	done := make(chan error, 1)
	go func() { done <- model.PublishPricingCatalogEntry(entry) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("publication did not reach source fence")
	}
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 250*time.Millisecond)
	err = writer.WithContext(writeCtx).Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error
	cancelWrite()
	assert.Error(t, err, "second real SQLite connection cannot mutate source between identity read and catalog commit")
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, writer.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error)
	changed := *channel
	changed.Models = "other/model"
	_, known := model.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}

func TestPricingCatalogBulkCommitKeepsOnlyMatchingLastGood(t *testing.T) {
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	require.NoError(t, model.PublishPricingCatalogEntry(entry))
	sibling := entry
	sibling.UpstreamModel = "upstream/sibling"
	sibling.Fingerprint = model.PricingCatalogSourceFingerprint(channel, sibling.UpstreamModel, sibling.Endpoint)
	require.NoError(t, model.PublishPricingCatalogEntry(sibling))
	stale := entry
	stale.Fingerprint = strings.Repeat("0", 64)
	count, err := model.PublishPricingCatalogContext(context.Background(), []model.PricingCatalogEntry{stale, sibling})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	_, known := model.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known, "a rejected staged source cannot replace a still-matching last good entry")
	_, known = model.GetPricingCatalogEntry(channel, sibling.UpstreamModel, sibling.Endpoint)
	assert.True(t, known)
	saved := model.RequireOptionValue(t, db, model.PricingCatalogOptionKey)
	snapshot, err := model.DecodePricingCatalogForTest(saved)
	require.NoError(t, err)
	assert.Len(t, snapshot.Entries, 2)
}

func TestPricingCatalogSourceMutationWaitsForPublication(t *testing.T) {
	if os.Getenv("GROUP_RATIO_TEST_MYSQL_DSN") == "" && os.Getenv("GROUP_RATIO_TEST_POSTGRES_DSN") == "" {
		t.Skip("real MySQL/PostgreSQL isolated DSN is not configured; SQLite lock is covered separately")
	}
	db, channel, entry := model.PricingCatalogOptionFixture(t)
	writer, err := gorm.Open(db.Dialector, &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { handle, err := writer.DB(); require.NoError(t, err); require.NoError(t, handle.Close()) })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:source_locked", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	t.Cleanup(func() { db.Callback().Query().Remove("test:source_locked") })
	done := make(chan error, 1)
	go func() { done <- model.PublishPricingCatalogEntry(entry) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("publication did not reach source fence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	err = writer.WithContext(ctx).Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error
	cancel()
	assert.Error(t, err, "source update must wait for publication's real database lock")
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, writer.Model(&model.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error)
	changed := *channel
	changed.Models = "other/model"
	_, known := model.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}
