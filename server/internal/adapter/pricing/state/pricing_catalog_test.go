package pricingstate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"
	settingsapp "github.com/QuantumNous/new-api/internal/app/settings"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func TestPricingCatalogOptionsPersistReloadAndRevision(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	before := pricingstate.GetPricingRevision()
	require.NoError(t, publishPricingCatalogEntry(entry))
	require.Greater(t, pricingstate.GetPricingRevision(), before)
	saved := requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey)
	assert.NotContains(t, saved, channel.Key)
	assert.False(t, pricingruntime.IsModelPricingOption(pricingdomain.PricingCatalogOptionKey))
	published, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, entry, published)
	revision := pricingstate.GetPricingRevision()
	_ = settingsapp.Default().Reload.Execute()
	assert.Equal(t, revision, pricingstate.GetPricingRevision())
	pricingstate.TestPricingCatalogState.Store(nil)
	_ = settingsapp.Default().Reload.Execute()
	reloaded, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, entry, reloaded)
	revision = pricingstate.GetPricingRevision()
	entry.FetchedAt = "2026-10-13T14:41:19Z"
	require.NoError(t, publishPricingCatalogEntry(entry))
	assert.Equal(t, revision, pricingstate.GetPricingRevision(), "timestamp-only metadata refresh preserves projection")
}

func TestPricingCatalogSourceSurvivesUnrelatedRoutes(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, publishPricingCatalogEntry(entry))
	mapping := `{"neighbor":"other/upstream","alias":"upstream/model"}`
	changed := *channel
	changed.Models = "neighbor,alias"
	changed.ModelMapping = &mapping
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"models": changed.Models, "model_mapping": mapping,
	}).Error)
	_, known := pricingstate.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known, "another route must not invalidate this upstream price")
	require.NoError(t, publishPricingCatalogEntry(entry), "unrelated route changes must not reject in-flight publication")

	remapped := `{"alias":"different/upstream"}`
	for _, change := range []struct {
		name    string
		channel legacy.Channel
	}{
		{"model removed", legacy.Channel{Models: "neighbor", ModelMapping: &mapping}},
		{"alias remapped", legacy.Channel{Models: "alias", ModelMapping: &remapped}},
	} {
		t.Run(change.name, func(t *testing.T) {
			replaced := changed
			replaced.Models = change.channel.Models
			replaced.ModelMapping = change.channel.ModelMapping
			_, known := pricingstate.GetPricingCatalogEntry(&replaced, entry.UpstreamModel, entry.Endpoint)
			assert.False(t, known)
		})
	}
	changed.Key = "rotated-fixture"
	_, known = pricingstate.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known, "credential changes must invalidate source prices")
}

func TestPricingCatalogLegacyFingerprintCompatibility(t *testing.T) {
	_, channel, entry := pricingCatalogOptionFixture(t)
	entry.Fingerprint = pricingstate.TestPricingCatalogFingerprint(
		channel,
		entry.UpstreamModel,
		entry.Endpoint,
		[]any{
			channel.Models,
			channel.GetModelMapping(),
		},
	)
	require.NoError(t, publishPricingCatalogEntry(entry))
	_, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known)
	changed := *channel
	changed.Models += ",neighbor"
	_, known = pricingstate.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known, "stale legacy fingerprints require fresh metadata")
}

func TestPricingCatalogOptionsRejectInvalidAndRetainSiblings(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, publishPricingCatalogEntry(entry))
	sibling := entry
	sibling.UpstreamModel = "upstream/other"
	sibling.Fingerprint = pricingstate.PricingCatalogSourceFingerprint(channel, sibling.UpstreamModel, sibling.Endpoint)
	require.NoError(t, publishPricingCatalogEntry(sibling))
	saved := requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey)
	revision := pricingstate.GetPricingRevision()
	invalid := strings.Replace(saved, `"amount_usd":0.03`, `"amount_usd":-1`, 1)
	require.Error(t, settingsapp.Default().Write.Execute(pricingdomain.PricingCatalogOptionKey, invalid))
	assert.Equal(t, saved, requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey))
	assert.Equal(t, revision, pricingstate.GetPricingRevision())
	_, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known)
	_, known = pricingstate.GetPricingCatalogEntry(channel, sibling.UpstreamModel, sibling.Endpoint)
	assert.True(t, known)
	require.NoError(t, db.Model(&legacy.Channel{}).Where("id = ?", channel.Id).Update("models", "changed-model").Error)
	require.ErrorContains(t, publishPricingCatalogEntry(entry), "source changed")
	assert.Equal(t, saved, requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey))
	changed := *channel
	changed.Models = "changed-model"
	_, known = pricingstate.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}

func TestPricingCatalogOptionsTransactionFailureDoesNotPublish(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, publishPricingCatalogEntry(entry))
	saved := requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:pricing_catalog_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(errors.New("pricing catalog transaction failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Update().Remove("test:pricing_catalog_fail") })
	revision := pricingstate.GetPricingRevision()
	entry.FetchedAt = "2026-10-13T15:41:19Z"
	require.Error(t, publishPricingCatalogEntry(entry))
	assert.Equal(t, saved, requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey))
	assert.Equal(t, revision, pricingstate.GetPricingRevision())
	retained, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	assert.Equal(t, "2026-10-13T13:41:19Z", retained.FetchedAt)
}

func TestPricingCatalogOptionsCancelledPublicationRetainsLastValid(t *testing.T) {
	db, _, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, publishPricingCatalogEntry(entry))
	previous := requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey)
	revision := pricingstate.GetPricingRevision()
	ctx, cancel := context.WithCancel(context.Background())
	unlockPublication := publication.LockOptionPublication()
	done := make(chan error, 1)
	go func() { done <- publishPricingCatalogEntryContext(ctx, entry) }()
	cancel()
	unlockPublication()
	require.ErrorIs(t, <-done, context.Canceled)
	assert.Equal(t, previous, requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey))
	assert.Equal(t, revision, pricingstate.GetPricingRevision())
}

func TestPricingCatalogOptionsInvalidPersistedStateRetainsLastValid(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, publishPricingCatalogEntry(entry))
	valid := requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey)
	revision := pricingstate.GetPricingRevision()
	for _, invalid := range []string{
		`{`,
		`{"version":0,"entries":{}}`,
		strings.Repeat(
			"x",
			pricingstate.TestPricingCatalogMaxBytes+1,
		),
		strings.Replace(
			valid,
			entry.FetchedAt,
			"invalid",
			1,
		),
	} {
		require.NoError(t, db.Model(&legacy.Option{}).Where(map[string]any{"key": pricingdomain.PricingCatalogOptionKey}).Update("value", invalid).Error)
		_ = settingsapp.Default().Reload.Execute()
		retained, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
		require.True(t, known)
		assert.Equal(t, entry, retained)
		assert.Equal(t, revision, pricingstate.GetPricingRevision())
		assert.Equal(t, valid, common.OptionMap[pricingdomain.PricingCatalogOptionKey])
	}
}

func TestPricingCatalogOptionsConcurrentMergeSyncPreservesBillingOptions(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	sentinel := legacy.Option{Key: "ModelRatio", Value: `{"financial-sentinel":7.125}`}
	require.NoError(t, db.Create(&sentinel).Error)
	previousRatio := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatio)) })
	second := entry
	second.UpstreamModel = "upstream/sibling"
	second.Fingerprint = pricingstate.PricingCatalogSourceFingerprint(channel, second.UpstreamModel, second.Endpoint)
	var workers sync.WaitGroup
	errors := make(chan error, 2)
	unlockPublication := publication.LockOptionPublication()
	workers.Go(func() { errors <- publishPricingCatalogEntry(entry) })
	workers.Go(func() { errors <- publishPricingCatalogEntry(second) })
	workers.Go(func() { _ = settingsapp.Default().Reload.Execute() })
	unlockPublication()
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	_, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	require.True(t, known)
	_, known = pricingstate.GetPricingCatalogEntry(channel, second.UpstreamModel, second.Endpoint)
	require.True(t, known)
	assert.Equal(t, sentinel.Value, requireOptionValue(t, db, sentinel.Key))
}

func TestImagePricingTokenInputsStayWithTheirProvider(t *testing.T) {
	amount := func(v float64) *float64 { return &v }
	image := pricingdomain.CatalogOffer{
		ID:            "43:google/model:studio:0:default",
		Operation:     "image_generation",
		Mode:          "sync",
		MainComponent: "image_output",
		Prices: []pricingdomain.CatalogPrice{{
			Component: "image_output",
			Unit:      "million_tokens",
			AmountUSD: amount(60),
		}},
	}
	tokens := pricingdomain.CatalogOffer{
		ID:            "43:google/model:studio:0:0",
		Operation:     "text",
		Mode:          "sync",
		MainComponent: "output",
		Prices: []pricingdomain.CatalogPrice{
			{
				Component: "input",
				Unit:      "million_tokens",
				AmountUSD: amount(.5),
			},
			{
				Component: "output",
				Unit:      "million_tokens",
				AmountUSD: amount(3),
			},
			{
				Component: "cache_read",
				Unit:      "million_tokens",
				AmountUSD: amount(0),
			},
		},
	}
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
			offers := []pricingdomain.CatalogOffer{image, source}
			before, err := common.Marshal(offers)
			require.NoError(t, err)
			result := pricingstate.TestImagePricingWithTokenInputs(offers)
			selected := pricingstate.SelectCatalogOffer(result[:1])
			require.NotNil(t, selected)
			prices := map[string]pricingdomain.CatalogPrice{}
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
		result := pricingstate.TestImagePricingWithTokenInputs([]pricingdomain.CatalogOffer{native, source})
		assert.Len(t, result[0].Prices, 1)
	})

	t.Run("native input wins", func(t *testing.T) {
		native := image
		native.Prices = append(slices.Clone(image.Prices), pricingdomain.CatalogPrice{Component: "input", Unit: "million_tokens", AmountUSD: amount(.7)})
		result := pricingstate.TestImagePricingWithTokenInputs([]pricingdomain.CatalogOffer{native, tokens})
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
		conditional.PublicConditions = []pricingdomain.ExpressionCondition{{Var: "len", Op: ">", Value: 100}}
		conditional.Prices = []pricingdomain.CatalogPrice{{Component: "input", Unit: "million_tokens", AmountUSD: amount(1)}}
		source := tokens
		source.PublicAlternatives = []pricingdomain.CatalogOffer{tokens, conditional}
		result := pricingstate.TestImagePricingWithTokenInputs([]pricingdomain.CatalogOffer{image, source})
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
	previousDB, previousState, previousOptions := legacy.DB, pricingstate.TestPricingCatalogState.Load(), common.OptionMap
	legacy.DB = db
	pricingstate.TestPricingCatalogState.Store(nil)
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		legacy.DB = previousDB
		pricingstate.TestPricingCatalogState.Store(previousState)
		common.OptionMap = previousOptions
		for _, handle := range []*gorm.DB{db, writer} {
			sqlDB, err := handle.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, db.AutoMigrate(&legacy.Channel{}, &legacy.Option{}))
	channel := &legacy.Channel{
		Id:     980,
		Type:   constant.ChannelTypeOpenRouter,
		Status: common.ChannelStatusEnabled,
		Key:    "fixture",
		Models: "upstream/model",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	amount := .01
	entry := pricingdomain.PricingCatalogEntry{
		ChannelID:     channel.Id,
		UpstreamModel: "upstream/model",
		Endpoint:      constant.EndpointTypeOpenAI,
		Source:        "https://openrouter.ai/api/v1/models/upstream/model/endpoints",
		FetchedAt:     "2026-10-15T00:00:00Z",
		Fingerprint: pricingstate.PricingCatalogSourceFingerprint(
			channel,
			"upstream/model",
			constant.EndpointTypeOpenAI,
		),
		Offers: []pricingdomain.CatalogOffer{{
			ID:            "native",
			Operation:     "text",
			Mode:          "sync",
			MainComponent: "input",
			Prices: []pricingdomain.CatalogPrice{{
				Component: "input",
				Unit:      "million_tokens",
				AmountUSD: &amount,
			}},
		}},
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:source_locked", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			once.Do(func() { close(entered); <-release })
		}
	}))
	done := make(chan error, 1)
	go func() { done <- publishPricingCatalogEntry(entry) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("publication did not reach source fence")
	}
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 250*time.Millisecond)
	err = writer.WithContext(writeCtx).Model(&legacy.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error
	cancelWrite()
	assert.Error(t, err, "second real SQLite connection cannot mutate source between identity read and catalog commit")
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, writer.Model(&legacy.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error)
	changed := *channel
	changed.Models = "other/model"
	_, known := pricingstate.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}

func TestPricingCatalogBulkCommitKeepsOnlyMatchingLastGood(t *testing.T) {
	db, channel, entry := pricingCatalogOptionFixture(t)
	require.NoError(t, publishPricingCatalogEntry(entry))
	sibling := entry
	sibling.UpstreamModel = "upstream/sibling"
	sibling.Fingerprint = pricingstate.PricingCatalogSourceFingerprint(channel, sibling.UpstreamModel, sibling.Endpoint)
	require.NoError(t, publishPricingCatalogEntry(sibling))
	stale := entry
	stale.Fingerprint = strings.Repeat("0", 64)
	count, err := pricingruntime.PublishPricingCatalogContext(context.Background(), []pricingdomain.PricingCatalogEntry{stale, sibling})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	_, known := pricingstate.GetPricingCatalogEntry(channel, entry.UpstreamModel, entry.Endpoint)
	assert.True(t, known, "a rejected staged source cannot replace a still-matching last good entry")
	_, known = pricingstate.GetPricingCatalogEntry(channel, sibling.UpstreamModel, sibling.Endpoint)
	assert.True(t, known)
	saved := requireOptionValue(t, db, pricingdomain.PricingCatalogOptionKey)
	snapshot, err := pricingstate.DecodePricingCatalog(saved)
	require.NoError(t, err)
	assert.Len(t, snapshot.Entries, 2)
}

func TestPricingCatalogSourceMutationWaitsForPublication(t *testing.T) {
	if os.Getenv("GROUP_RATIO_TEST_MYSQL_DSN") == "" && os.Getenv("GROUP_RATIO_TEST_POSTGRES_DSN") == "" {
		t.Skip("real MySQL/PostgreSQL isolated DSN is not configured; SQLite lock is covered separately")
	}
	db, channel, entry := pricingCatalogOptionFixture(t)
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
	go func() { done <- publishPricingCatalogEntry(entry) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("publication did not reach source fence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	err = writer.WithContext(ctx).Model(&legacy.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error
	cancel()
	assert.Error(t, err, "source update must wait for publication's real database lock")
	close(release)
	require.NoError(t, <-done)
	require.NoError(t, writer.Model(&legacy.Channel{}).Where("id = ?", channel.Id).Update("models", "other/model").Error)
	changed := *channel
	changed.Models = "other/model"
	_, known := pricingstate.GetPricingCatalogEntry(&changed, entry.UpstreamModel, entry.Endpoint)
	assert.False(t, known)
}
