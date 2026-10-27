package catalog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	legacy "github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCatalogPersistence(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			previous := common.MainDatabaseType()
			t.Cleanup(func() { common.SetMainDatabaseType(previous) })
			var dialector gorm.Dialector
			if engine == "postgres" {
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not set")
				}
				dialector = postgres.Open(pgfixture.ScopedDSN(t, dsn))
				common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
			} else {
				dialector = sqlite.Open(filepath.Join(t.TempDir(), "catalog.db"))
				common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			}
			open := func() *gorm.DB {
				db, err := gorm.Open(dialector, &gorm.Config{})
				require.NoError(t, err)
				pool, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, pool.Close()) })
				if engine == "postgres" {
					pgfixture.ProbePool(t, db)
				}
				return db
			}
			database := open()
			for range 2 {
				require.NoError(t, database.AutoMigrate(&record.PrefillGroup{}, &legacy.Channel{}, &channelrecord.Ability{}))
			}
			var version string
			query := "SELECT sqlite_version()"
			if engine == "postgres" {
				query = "SHOW server_version"
			}
			require.NoError(t, database.Raw(query).Scan(&version).Error)
			t.Logf("%s version %s", engine, version)
			groups := NewPrefillStore(func() *gorm.DB { return database })
			items := [][]byte{nil, []byte("null"), []byte("[]"), []byte("{}"), []byte(`[0,false,"raw"]`), []byte("42")}
			ids := make([]int, len(items))
			for i, raw := range items {
				value := domain.PrefillGroup{Name: fmt.Sprintf("roundtrip-%d", i), Type: "model", Items: raw, CreatedTime: 11, UpdatedTime: int64(20 + i)}
				require.NoError(t, groups.Insert(&value))
				require.Positive(t, value.Id)
				ids[i] = value.Id
			}
			rows, err := groups.List("model")
			require.NoError(t, err)
			require.Len(t, rows, len(items))
			assert.Equal(t, ids[len(ids)-1], rows[0].Id)
			for _, v := range rows {
				for i, id := range ids {
					if v.Id == id {
						assert.Equal(t, items[i], v.Items)
					}
				}
			}
			duplicate, err := groups.Duplicate(0, "roundtrip-0")
			require.NoError(t, err)
			assert.True(t, duplicate)
			// Whole-record Save keeps zero and empty input fields, and inserts a missing primary key.
			value := domain.PrefillGroup{Id: ids[0]}
			require.NoError(t, groups.Save(&value))
			var saved record.PrefillGroup
			require.NoError(t, database.First(&saved, ids[0]).Error)
			assert.Empty(t, saved.Name)
			assert.Zero(t, saved.CreatedTime)
			assert.Nil(t, saved.Items)
			missing := domain.PrefillGroup{Id: 101, Name: "upsert", Type: "tag", Items: []byte("false")}
			require.NoError(t, groups.Save(&missing))
			saved = record.PrefillGroup{}
			require.NoError(t, database.First(&saved, 101).Error)
			assert.Equal(t, "upsert", saved.Name)
			require.NoError(t, groups.Delete(ids[1]))
			var deleted record.PrefillGroup
			require.NoError(t, database.Unscoped().First(&deleted, ids[1]).Error)
			assert.True(t, deleted.DeletedAt.Valid)
			replacement := domain.PrefillGroup{Name: "roundtrip-1", Type: "model"}
			require.NoError(t, groups.Insert(&replacement))
			// Owner/reserve reads retain group quoting, priority order and positive reserve selection.
			settings := `{"openrouter_model_reserve_usd":{"wave6c-model":0.05}}`
			priority := int64(2)
			require.NoError(t, database.Create(&legacy.Channel{Id: 31, Type: constant.ChannelTypeOpenRouter, Key: "fixture", Status: common.ChannelStatusEnabled, Setting: &settings}).Error)
			require.NoError(t, database.Create(&channelrecord.Ability{Group: "default", Model: "wave6c-model", ChannelId: 31, Enabled: true, Priority: &priority}).Error)
			groupColumn := "`group`"
			if engine == "postgres" {
				groupColumn = `"group"`
			}
			availability := NewAvailabilityStore(func() *gorm.DB { return database }, func() string { return groupColumn })
			owners, err := availability.PreferredOwners([]string{" wave6c-model ", "wave6c-model"}, []string{"default"})
			require.NoError(t, err)
			assert.Equal(t, map[string]int{"wave6c-model": constant.ChannelTypeOpenRouter}, owners)
			reserve, err := availability.ReserveBilled([]string{"wave6c-model"}, []string{"default"})
			require.NoError(t, err)
			assert.True(t, reserve["wave6c-model"])
			reserve, err = availability.ReserveBilled([]string{"wave6c-model"}, []string{"vip"})
			require.NoError(t, err)
			assert.Empty(t, reserve)
			pool, err := database.DB()
			require.NoError(t, err)
			require.NoError(t, pool.Close())
			database = open()
			rows, err = groups.List("tag")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, []byte("false"), rows[0].Items)
		})
	}
}

func TestPrefillJSONValueOwnsDriverBytes(t *testing.T) {
	raw := []byte(`[1,"x"]`)
	var value record.JSONValue
	require.NoError(t, value.Scan(raw))
	raw[1] = '2'
	assert.Equal(t, []byte(`[1,"x"]`), []byte(value))
	sqlValue, err := value.Value()
	require.NoError(t, err)
	assert.Equal(t, `[1,"x"]`, sqlValue)
	require.NoError(t, value.Scan(nil))
	sqlValue, err = value.Value()
	require.NoError(t, err)
	assert.Nil(t, sqlValue)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	assert.True(t, bytes.Equal([]byte("null"), encoded))
	require.NoError(t, value.Scan(map[string]int{"zero": 0}))
	assert.Equal(t, []byte(`{"zero":0}`), []byte(value))
}
