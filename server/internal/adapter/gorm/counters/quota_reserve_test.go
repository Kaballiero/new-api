package counters_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	keycache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	cachequota "github.com/QuantumNous/new-api/internal/adapter/cache/quota"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/counters"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/app/quotaruntime"
	"github.com/QuantumNous/new-api/internal/contracttest/cachefixture"
	"github.com/QuantumNous/new-api/model"
)

func createReserveTestUser(t *testing.T, quota int) model.User {
	t.Helper()
	user := model.User{
		Username:    "reserve-user-" + common.GetRandomString(6),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       quota,
		AffCode:     "reserve-aff-" + common.GetRandomString(8),
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func createReserveTestToken(t *testing.T, remainQuota int) model.Token {
	t.Helper()
	token := model.Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, apikeystorage.Persistence().Insert(&token))
	return token
}

func getUserQuotaFromDB(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").First(&user, id).Error)
	return user.Quota
}

func getTokenFromDB(t *testing.T, id int) model.Token {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.First(&token, id).Error)
	return token
}

func resetBatchUpdateTestState(t *testing.T) {
	t.Helper()
	oldBatchEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	for i := range counters.BatchUpdateTypeCount {
		counters.BatchUpdateLocks()[i].Lock()
		counters.BatchUpdateStores()[i] = make(map[int]int)
		counters.BatchUpdateLocks()[i].Unlock()
	}
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchEnabled
		for i := range counters.BatchUpdateTypeCount {
			counters.BatchUpdateLocks()[i].Lock()
			counters.BatchUpdateStores()[i] = make(map[int]int)
			counters.BatchUpdateLocks()[i].Unlock()
		}
	})
}

func TestTryReserveQuotaWithoutRedis(t *testing.T) {
	cachefixture.Open(t)
	resetBatchUpdateTestState(t)

	user := createReserveTestUser(t, 100)
	reserved, err := quotaruntime.Reserves().TryReserveUserQuota(user.Id, 60)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	reserved, err = quotaruntime.Reserves().TryReserveUserQuota(user.Id, 41)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	token := createReserveTestToken(t, 80)
	reserved, err = quotaruntime.Reserves().TryReserveTokenQuota(token.Id, token.Key, 25, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reloaded := getTokenFromDB(t, token.Id)
	assert.Equal(t, 55, reloaded.RemainQuota)
	assert.Equal(t, 25, reloaded.UsedQuota)

	reserved, err = quotaruntime.Reserves().TryReserveTokenQuota(token.Id, token.Key, 56, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 55, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestRedisBatchReserveNeverFallsBackToStaleDatabaseBalance(t *testing.T) {
	cachefixture.Open(t)
	resetBatchUpdateTestState(t)
	cachefixture.NewRedis(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, 10)
	reserved, err := quotaruntime.Reserves().TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 10, getUserQuotaFromDB(t, user.Id), "batch delta is not flushed yet")

	reserved, err = quotaruntime.Reserves().TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	assert.False(t, reserved, "stale DB balance must not authorize a second spend")
	cachedUser, err := accountstorage.Cache().GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, cachedUser.Quota)

	token := createReserveTestToken(t, 9)
	reserved, err = quotaruntime.Reserves().TryReserveTokenQuota(token.Id, token.Key, 7, false)
	require.NoError(t, err)
	assert.True(t, reserved)
	reserved, err = quotaruntime.Reserves().TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 9, getTokenFromDB(t, token.Id).RemainQuota)

	counters.FlushBatchUpdates(func() *gorm.DB { return model.DB })
	assert.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 2, reloadedToken.RemainQuota)
	assert.Equal(t, 7, reloadedToken.UsedQuota)
}

func TestBatchUpdateAccumulatesTwoMaximumRequestCharges(t *testing.T) {
	cachefixture.Open(t)
	resetBatchUpdateTestState(t)
	common.BatchUpdateEnabled = true

	user := createReserveTestUser(t, common.MaxQuota*2+100)
	require.NoError(t, accountstorage.SQL().DecreaseUserQuota(user.Id, common.MaxQuota, false))
	require.NoError(t, accountstorage.SQL().DecreaseUserQuota(user.Id, common.MaxQuota, false))

	counters.FlushBatchUpdates(func() *gorm.DB { return model.DB })
	assert.Equal(t, 100, getUserQuotaFromDB(t, user.Id))
}

func TestBatchUpdateAccumulatorSaturatesOverflow(t *testing.T) {
	resetBatchUpdateTestState(t)

	counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, 1, math.MaxInt)
	counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, 1, 1)
	counters.BatchUpdateLocks()[counters.BatchUpdateTypeUserQuota].Lock()
	assert.Equal(t, math.MaxInt, counters.BatchUpdateStores()[counters.BatchUpdateTypeUserQuota][1])
	counters.BatchUpdateLocks()[counters.BatchUpdateTypeUserQuota].Unlock()

	counters.BatchUpdateLocks()[counters.BatchUpdateTypeUserQuota].Lock()
	counters.BatchUpdateStores()[counters.BatchUpdateTypeUserQuota] = make(map[int]int)
	counters.BatchUpdateLocks()[counters.BatchUpdateTypeUserQuota].Unlock()
	counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, 1, math.MinInt)
	counters.AddNewRecord(counters.BatchUpdateTypeUserQuota, 1, -1)
	counters.BatchUpdateLocks()[counters.BatchUpdateTypeUserQuota].Lock()
	assert.Equal(t, math.MinInt, counters.BatchUpdateStores()[counters.BatchUpdateTypeUserQuota][1])
	counters.BatchUpdateLocks()[counters.BatchUpdateTypeUserQuota].Unlock()
}

func TestReserveFallsBackToDatabaseWhenRedisIsUnavailable(t *testing.T) {
	cachefixture.Open(t)
	resetBatchUpdateTestState(t)
	server := cachefixture.NewRedis(t)

	user := createReserveTestUser(t, 20)
	require.NoError(t, accountstorage.Cache().PopulateUserCache(user))
	server.Close()

	// Redis failures fall back to conditional database updates without overspending or losing availability.
	reserved, err := quotaruntime.Reserves().TryReserveUserQuota(user.Id, 5)
	require.NoError(t, err)
	assert.True(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))

	reserved, err = quotaruntime.Reserves().TryReserveUserQuota(user.Id, 16)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))
}

func TestSynchronousReserveCompensatesCacheWhenPersistenceFails(t *testing.T) {
	cachefixture.Open(t)
	resetBatchUpdateTestState(t)
	cachefixture.NewRedis(t)

	user := createReserveTestUser(t, 10)
	require.NoError(t, accountstorage.Cache().PopulateUserCache(user))
	require.NoError(t, model.DB.Delete(&user).Error)

	reserved, err := quotaruntime.Reserves().TryReserveUserQuota(user.Id, 6)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cached, cacheErr := accountstorage.Cache().CacheGetUserBase(user.Id)
	require.NoError(t, cacheErr)
	assert.Equal(t, 10, cached.Quota)

	token := createReserveTestToken(t, 12)
	_, err = apikeystorage.Reader().Read(token.Key, true)
	require.NoError(t, err)
	require.NoError(t, model.DB.Delete(&token).Error)
	reserved, err = quotaruntime.Reserves().TryReserveTokenQuota(token.Id, token.Key, 7, false)
	assert.False(t, reserved)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	cachedToken, cacheErr := keycache.CacheGetTokenByKey(token.Key)
	require.NoError(t, cacheErr)
	assert.Equal(t, 12, cachedToken.RemainQuota)
	assert.Zero(t, cachedToken.UsedQuota)
}

func TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot(t *testing.T) {
	cachefixture.Open(t)
	resetBatchUpdateTestState(t)
	server := cachefixture.NewRedis(t)

	token := createReserveTestToken(t, 100)
	loaded, err := apikeystorage.Reader().Read(token.Key, true)
	require.NoError(t, err)
	stale := *loaded

	result, err := keycache.CacheApplyTokenQuotaDelta(token.Id, token.Key, -70)
	require.NoError(t, err)
	require.Equal(t, cachequota.OK, result)

	// An existing hash only refreshes its TTL; a database snapshot must not overwrite an atomically pre-consumed balance.
	code, err := keycache.CacheInitToken(stale)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	cached, err := keycache.CacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 30, cached.RemainQuota)

	// During mutation, the fence deletes cache data and blocks stale snapshots held by concurrent readers.
	require.NoError(t, keycache.InvalidateTokenCacheForMutation(token.Key))
	code, err = keycache.CacheInitToken(stale)
	require.NoError(t, err)
	assert.Zero(t, code, "the pre-mutation snapshot must not be published while fenced")
	_, err = keycache.CacheGetTokenByKey(token.Key)
	assert.Error(t, err)

	// Hydration from the database resumes after the fence expires.
	server.FastForward(time.Duration(10+1) * time.Second)
	fresh, err := apikeystorage.Reader().Read(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 100, fresh.RemainQuota)
	cached, err = keycache.CacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, 100, cached.RemainQuota)
}
