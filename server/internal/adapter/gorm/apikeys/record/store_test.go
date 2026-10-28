package model_test

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	accountsrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/audit/audittest"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openStoreDatabase opens SQLite, or MySQL/PostgreSQL from TEST_MYSQL_DSN /
// TEST_POSTGRES_DSN in an isolated database or schema. Each connection pool
// has several connections so that concurrent transactions really overlap.
func openStoreDatabase(t *testing.T, kind string) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	switch kind {
	case "sqlite":
		// Deferred transactions (no _txlock=immediate): the owner lock itself
		// must serialize writers.
		var err error
		db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "tokens.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)"), &gorm.Config{})
		require.NoError(t, err)
	default:
		env := map[string]string{"mysql": "TEST_MYSQL_DSN", "postgres": "TEST_POSTGRES_DSN"}[kind]
		dsn := os.Getenv(env)
		if dsn == "" {
			t.Skip(env + " is not configured")
		}
		db, _ = audittest.NewDatabase(t, kind, dsn)
	}
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = conn.Close() })
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseType(kind), previousLog)
	t.Cleanup(func() { common.SetDatabaseTypes(previousMain, previousLog) })
	require.NoError(t, db.AutoMigrate(&accountsrecord.User{}, &record.Token{}))
	return db
}

func newStore(db *gorm.DB) record.Store {
	return record.NewStore(func() *gorm.DB { return db }, func() string { return "key" },
		func(string) error { return nil }, func([]record.Token) error { return nil })
}

func createUser(t *testing.T, db *gorm.DB, name string) int {
	t.Helper()
	user := accountsrecord.User{Username: name, Password: "placeholder", AffCode: name}
	require.NoError(t, db.Create(&user).Error)
	return user.Id
}

var databases = []string{"sqlite", "mysql", "postgres"}

func TestStoreListsKeysThatAreNotDeleted(t *testing.T) {
	for _, kind := range databases {
		t.Run(kind, func(t *testing.T) {
			db := openStoreDatabase(t, kind)
			store := newStore(db)
			owner, other := createUser(t, db, "list-owner"), createUser(t, db, "list-other")
			var ownerIDs, otherIDs []int
			for i := range 5 {
				token := record.Token{UserId: owner, Name: "owner", Key: "owner-key-" + string(rune('a'+i))}
				require.NoError(t, store.Insert(&token))
				ownerIDs = append(ownerIDs, token.Id)
				foreign := record.Token{UserId: other, Name: "other", Key: "other-key-" + string(rune('a'+i))}
				require.NoError(t, store.Insert(&foreign))
				otherIDs = append(otherIDs, foreign.Id)
			}
			require.NoError(t, db.Delete(&record.Token{Id: ownerIDs[2]}).Error)

			ids := func(tokens []*record.Token) []int {
				result := []int{}
				for _, token := range tokens {
					result = append(result, token.Id)
				}
				return result
			}
			first, err := store.ListAfter(owner, 0, 2)
			require.NoError(t, err)
			assert.Equal(t, []int{ownerIDs[4], ownerIDs[3]}, ids(first))
			second, err := store.ListAfter(owner, ownerIDs[3], 2)
			require.NoError(t, err)
			assert.Equal(t, []int{ownerIDs[1], ownerIDs[0]}, ids(second), "the deleted key is skipped")
			rest, err := store.ListAfter(owner, ownerIDs[0], 2)
			require.NoError(t, err)
			assert.Empty(t, rest)
			unknown, err := store.ListAfter(999999, 0, 100)
			require.NoError(t, err)
			assert.Empty(t, unknown)
			total, err := store.Count(owner)
			require.NoError(t, err)
			assert.EqualValues(t, 4, total)

			byIDs, err := store.ListByIDs([]int{ownerIDs[0], otherIDs[1], ownerIDs[2], 999999})
			require.NoError(t, err)
			assert.Equal(t, []int{otherIDs[1], ownerIDs[0]}, ids(byIDs), "any user, newest first, without deleted or unknown keys")
		})
	}
}

func TestStoreKeepsKeyNamesUniquePerUser(t *testing.T) {
	for _, kind := range databases {
		t.Run(kind, func(t *testing.T) {
			db := openStoreDatabase(t, kind)
			store := newStore(db)
			owner, other := createUser(t, db, "name-owner"), createUser(t, db, "name-other")
			insert := func(userID int, name, key string) (record.Token, error) {
				token := record.Token{UserId: userID, Name: name, Key: key}
				return token, store.InsertWithUniqueName(&token)
			}
			prod, err := insert(owner, "Prod", "key-prod")
			require.NoError(t, err)
			_, err = insert(owner, "Прод", "key-cyrillic")
			require.NoError(t, err)

			for _, name := range []string{"Prod", "prod", "PROD", " prod ", "прОД"} {
				_, err := insert(owner, name, "key-duplicate")
				assert.ErrorIs(t, err, domain.ErrNameDuplicate, name)
			}
			_, err = insert(other, "prod", "key-other-user")
			assert.NoError(t, err, "another user may use the name")

			// A deleted key does not occupy its name.
			deleted, err := insert(owner, "Staging", "key-staging")
			require.NoError(t, err)
			require.NoError(t, db.Delete(&deleted).Error)
			_, err = insert(owner, "staging", "key-staging-new")
			require.NoError(t, err)

			// The renamed key itself does not occupy the name; others do.
			prod.Name = "PROD"
			require.NoError(t, store.UpdateWithUniqueName(&prod))
			prod.Name = "staging"
			assert.ErrorIs(t, store.UpdateWithUniqueName(&prod), domain.ErrNameDuplicate)
			var stored record.Token
			require.NoError(t, db.First(&stored, prod.Id).Error)
			assert.Equal(t, "PROD", stored.Name)

			var names []string
			require.NoError(t, db.Model(&record.Token{}).Where("user_id = ?", owner).Order("id").Pluck("name", &names).Error)
			assert.Equal(t, []string{"PROD", "Прод", "staging"}, names)
		})
	}
}

// An edit that read the key before a concurrent rename must not write the old
// name back: plain Update leaves the name to UpdateWithUniqueName.
func TestStoreUpdateDoesNotRestoreStaleName(t *testing.T) {
	for _, kind := range databases {
		t.Run(kind, func(t *testing.T) {
			db := openStoreDatabase(t, kind)
			store := newStore(db)
			owner := createUser(t, db, "stale-owner")
			key := record.Token{UserId: owner, Name: "A", Key: "stale-key", Status: 1}
			require.NoError(t, store.InsertWithUniqueName(&key))
			stale := key // a pause read the key with name "A"

			renamed := key
			renamed.Name = "B"
			require.NoError(t, store.UpdateWithUniqueName(&renamed))
			fresh := record.Token{UserId: owner, Name: "A", Key: "fresh-key"}
			require.NoError(t, store.InsertWithUniqueName(&fresh), "the old name is free after the rename")

			stale.Status = 2
			require.NoError(t, store.Update(&stale))
			var stored record.Token
			require.NoError(t, db.First(&stored, key.Id).Error)
			assert.Equal(t, "B", stored.Name, "the rename survives")
			assert.Equal(t, 2, stored.Status, "the other fields are written")
			var names []string
			require.NoError(t, db.Model(&record.Token{}).Where("user_id = ?", owner).Order("id").Pluck("name", &names).Error)
			assert.Equal(t, []string{"B", "A"}, names)
		})
	}
}

// Both transactions are held at the name read until the other one arrives
// there too. With a working owner lock the second one cannot arrive before
// the first commits, so the first proceeds after the wait and the second sees
// its key.
func TestStoreSerializesConcurrentInsertsOfOneName(t *testing.T) {
	for _, kind := range databases {
		t.Run(kind, func(t *testing.T) {
			db := openStoreDatabase(t, kind)
			store := newStore(db)
			owner := createUser(t, db, "race-owner")
			var arrived atomic.Int32
			both := make(chan struct{})
			const callback = "test:token_name_race"
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if _, names := tx.Statement.Dest.(*[]string); !names || tx.Statement.Table != "tokens" {
					return
				}
				if arrived.Add(1) == 2 {
					close(both)
					return
				}
				select {
				case <-both:
				case <-time.After(time.Second):
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })

			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Go(func() {
					token := record.Token{UserId: owner, Name: "Prod", Key: "race-key-" + string(rune('a'+i))}
					errs[i] = store.InsertWithUniqueName(&token)
				})
			}
			wg.Wait()

			succeeded, duplicates := 0, 0
			for _, err := range errs {
				switch {
				case err == nil:
					succeeded++
				case assert.ErrorIs(t, err, domain.ErrNameDuplicate):
					duplicates++
				}
			}
			assert.Equal(t, 1, succeeded)
			assert.Equal(t, 1, duplicates)
			var count int64
			require.NoError(t, db.Model(&record.Token{}).Where("user_id = ?", owner).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}
