package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	keycache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	cachequota "github.com/QuantumNous/new-api/internal/adapter/cache/quota"
	keyrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/go-redis/redis/v8"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/contracttest/cachefixture"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserAuthFenceRollbackExpiresAndRecovers(t *testing.T) {
	setupCacheDatabase(t)
	server := cachefixture.NewRedis(t)

	user := User{
		Username:    "auth-fence-rollback",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, testStore.PopulateUserCache(user))

	tx := DB.Begin()
	require.NoError(t, tx.Error)
	next, err := testStore.IncrementUserAuthVersionWithTx(tx, user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2, next)

	_, err = testStore.CacheGetUserBase(user.Id)
	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	cacheTTL, err := common.RDB.TTL(t.Context(), GetUserCacheKey(user.Id)).Result()
	require.NoError(t, err)
	fenceTTL, err := common.RDB.TTL(t.Context(), getUserAuthFenceKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Greater(t, fenceTTL, cacheTTL)
	require.NoError(t, tx.Rollback().Error)

	server.FastForward(time.Duration(userAuthFenceTTLSeconds()+1) * time.Second)
	assert.False(t, server.Exists(getUserAuthFenceKey(user.Id)))
	committed, err := common.RDB.Get(t.Context(), getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "1", committed)

	cached, err := testStore.GetUserCache(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1, cached.AuthVersion)
}

func TestPendingUserAuthFenceRejectsStaleCacheWrite(t *testing.T) {
	server := cachefixture.NewRedis(t)
	const userID = 4201
	require.NoError(t, testStore.SetUserAuthVersionFence(userID, 2))

	err := testStore.writeUserCache(&UserBase{
		Id: userID, Group: "default", Username: "stale", AuthVersion: 1,
	}, true)

	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	assert.False(t, server.Exists(GetUserCacheKey(userID)))
}

func TestUserAuthFieldUpdateRejectsVersionMismatch(t *testing.T) {
	cachefixture.NewRedis(t)
	const userID = 4202
	require.NoError(t, testStore.writeUserCache(&UserBase{
		Id: userID, Group: "current", Username: "cached", AuthVersion: 3,
	}, true))

	err := testStore.updateUserCacheFieldAtVersion(userID, "Group", "stale", 2)

	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	group, err := common.RDB.HGet(t.Context(), GetUserCacheKey(userID), "Group").Result()
	require.NoError(t, err)
	assert.Equal(t, "current", group)
}

func TestRefreshUserGroupCacheRepairsDelayedSameVersionWrite(t *testing.T) {
	setupCacheDatabase(t)
	cachefixture.NewRedis(t)

	user := User{
		Username:    "delayed-group-refresh",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, testStore.PopulateUserCache(user))

	firstSnapshotRead := make(chan struct{})
	releaseDelayedRefresh := make(chan struct{})
	var intercepted atomic.Bool
	const callbackName = "test:block_delayed_group_refresh"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(callbackName, func(*gorm.DB) {
		if intercepted.CompareAndSwap(false, true) {
			close(firstSnapshotRead)
			<-releaseDelayedRefresh
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Query().Remove(callbackName)
	})

	delayedResult := make(chan error, 1)
	go func() {
		delayedResult <- testStore.RefreshUserGroupCache(user.Id)
	}()
	<-firstSnapshotRead

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "pro").Error)
	require.NoError(t, testStore.RefreshUserGroupCache(user.Id))
	cached, err := testStore.CacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pro", cached.Group)
	assert.EqualValues(t, 1, cached.AuthVersion)

	close(releaseDelayedRefresh)
	require.NoError(t, <-delayedResult)
	cached, err = testStore.CacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pro", cached.Group)
	assert.EqualValues(t, 1, cached.AuthVersion)
}

func TestCommittedUserAuthVersionPermanentlyRejectsDelayedCacheFill(t *testing.T) {
	setupCacheDatabase(t)
	server := cachefixture.NewRedis(t)

	user := User{
		Username:    "auth-fence-commit",
		Password:    "password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, testStore.PopulateUserCache(user))
	stale := *ToBaseUser(&user)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := testStore.IncrementUserAuthVersionWithTx(tx, user.Id)
		return err
	}))
	require.NoError(t, testStore.PublishUserAuthCache(user.Id))
	assert.False(t, server.Exists(getUserAuthFenceKey(user.Id)))
	committed, err := common.RDB.Get(t.Context(), getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "2", committed)

	server.FastForward(time.Duration(userAuthFenceTTLSeconds()+1) * time.Second)
	require.NoError(t, common.RedisDelKey(GetUserCacheKey(user.Id)))
	err = testStore.writeUserCache(&stale, true)
	assert.True(t, errors.Is(err, ErrUserAuthCachePending))
	committed, err = common.RDB.Get(t.Context(), getUserAuthVersionKey(user.Id)).Result()
	require.NoError(t, err)
	assert.Equal(t, "2", committed)
}

func TestUserAuthVersionFenceAndCommittedFloorAreMonotonic(t *testing.T) {
	setupCacheDatabase(t)
	server := cachefixture.NewRedis(t)

	const userID = 4101
	require.NoError(t, testStore.SetUserAuthVersionFence(userID, 5))
	require.NoError(t, testStore.SetUserAuthVersionFence(userID, 3))
	pending, err := common.RDB.Get(t.Context(), getUserAuthFenceKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", pending)
	floor, err := testStore.getUserAuthVersionFloor(userID)
	require.NoError(t, err)
	assert.EqualValues(t, 5, floor)

	// Committing an older transaction must neither clear a newer pending fence
	// nor lower the effective deny floor.
	require.NoError(t, testStore.PublishCommittedUserAuthVersion(userID, 3))
	pending, err = common.RDB.Get(t.Context(), getUserAuthFenceKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", pending)
	floor, err = testStore.getUserAuthVersionFloor(userID)
	require.NoError(t, err)
	assert.EqualValues(t, 5, floor)

	require.NoError(t, testStore.PublishCommittedUserAuthVersion(userID, 5))
	assert.False(t, server.Exists(getUserAuthFenceKey(userID)))
	committed, err := common.RDB.Get(t.Context(), getUserAuthVersionKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", committed)

	require.NoError(t, testStore.PublishCommittedUserAuthVersion(userID, 4))
	committed, err = common.RDB.Get(t.Context(), getUserAuthVersionKey(userID)).Result()
	require.NoError(t, err)
	assert.Equal(t, "5", committed)
}

var DB *gorm.DB
var testStore Store

type User = record.User

func setupCacheDatabase(t *testing.T) {
	t.Helper()
	DB = cachefixture.Open(t)
	queries := record.NewQueries(func() *gorm.DB { return DB }, func() string { return "`group`" })
	testStore = NewStore(Environment{DB: func() *gorm.DB { return DB }, GroupColumn: func() string { return "`group`" }, GetUserByID: queries.GetUserById, LockForUpdate: locking.LockForUpdate})
}

// TestSharedQuotaCacheRealRedis runs the actual Lua and mutation fences on a real Redis server.
func TestSharedQuotaCacheRealRedis(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is required for real Redis fences")
	}
	client := redis.NewClient(&redis.Options{Addr: address, Password: os.Getenv("TEST_REDIS_PASSWORD"), MaxRetries: -1})
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = oldClient, oldEnabled; assert.NoError(t, client.Close()) })
	require.NoError(t, client.Ping(t.Context()).Err())
	info, err := client.Info(t.Context(), "server").Result()
	require.NoError(t, err)
	var version string
	for line := range strings.SplitSeq(info, "\n") {
		if value, ok := strings.CutPrefix(line, "redis_version:"); ok {
			version = strings.TrimSpace(value)
		}
	}
	require.NotEmpty(t, version)
	t.Logf("Redis %s; isolated account and token keys", version)
	// A unique fixture identity isolates the unchanged production namespaces without FLUSHDB.
	digest := sha256.Sum256([]byte(common.GetUUID()))
	userID := int(binary.BigEndian.Uint64(digest[:8])&((1<<52)-1)) + 1
	tokenKey := "wave7j-real-" + common.GetUUID()
	keys := []string{GetUserCacheKey(userID), getUserAuthFenceKey(userID), getUserAuthVersionKey(userID),
		keycache.GetTokenCacheKey(tokenKey), keycache.GetTokenCacheFenceKey(tokenKey)}
	t.Cleanup(func() { assert.NoError(t, client.Del(context.Background(), keys...).Err()) })
	store := Store{}
	user := UserBase{Id: userID, Group: "default", Username: "real-redis-fixture", AuthVersion: 1, Quota: 100}
	require.NoError(t, store.writeUserCache(&user, true))
	outcome, err := CacheTryReserveUserQuota(userID, 40)
	require.NoError(t, err)
	assert.Equal(t, cachequota.OK, outcome)
	cached, err := store.CacheGetUserBase(userID)
	require.NoError(t, err)
	assert.Equal(t, 60, cached.Quota)
	outcome, err = CacheTryReserveUserQuota(userID, 61)
	require.NoError(t, err)
	assert.Equal(t, cachequota.Insufficient, outcome)
	require.NoError(t, store.SetUserAuthVersionFence(userID, 2))
	_, err = store.CacheGetUserBase(userID)
	assert.ErrorIs(t, err, ErrUserAuthCachePending)
	assert.ErrorIs(t, store.writeUserCache(&user, true), ErrUserAuthCachePending)
	require.NoError(t, store.PublishCommittedUserAuthVersion(userID, 2))
	user.AuthVersion = 2
	require.NoError(t, store.writeUserCache(&user, false))
	cached, err = store.CacheGetUserBase(userID)
	require.NoError(t, err)
	assert.Equal(t, 60, cached.Quota, "a security publication must preserve the live pre-consumed quota")
	assert.EqualValues(t, 2, cached.AuthVersion)
	require.NoError(t, client.Del(t.Context(), GetUserCacheKey(userID)).Err())
	outcome, err = CacheApplyUserQuotaDelta(userID, 20)
	require.NoError(t, err)
	assert.Equal(t, cachequota.Miss, outcome)
	exists, err := client.Exists(t.Context(), GetUserCacheKey(userID)).Result()
	require.NoError(t, err)
	assert.Zero(t, exists, "a missing hash must not become a partial quota hash")
	token := keyrecord.Token{Id: userID, UserId: userID, Key: tokenKey, Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100}
	initialized, err := keycache.CacheInitToken(token)
	require.NoError(t, err)
	assert.Equal(t, 1, initialized)
	outcome, err = keycache.CacheTryReserveTokenQuota(token.Id, token.Key, 70)
	require.NoError(t, err)
	assert.Equal(t, cachequota.OK, outcome)
	initialized, err = keycache.CacheInitToken(token)
	require.NoError(t, err)
	assert.Equal(t, 2, initialized)
	cachedToken, err := keycache.CacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cachedToken.RemainQuota)
	assert.Equal(t, 70, cachedToken.UsedQuota)
	require.NoError(t, keycache.InvalidateTokenCacheForMutation(token.Key))
	initialized, err = keycache.CacheInitToken(token)
	require.NoError(t, err)
	assert.Zero(t, initialized, "an in-flight database snapshot must stay fenced")
	_, err = keycache.CacheGetTokenByKey(token.Key)
	assert.Error(t, err)
	ttl, err := client.TTL(t.Context(), keycache.GetTokenCacheFenceKey(token.Key)).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 10*time.Second)
}
