package apikeys_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	cache "github.com/QuantumNous/new-api/internal/adapter/cache/apikeys"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/contracttest/cachefixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenAutoGroupsRoundTripThroughRedisHashCache(t *testing.T) {
	cachefixture.NewRedis(t)
	token := record.Token{
		Id:         42,
		UserId:     7,
		Key:        "token-auto-groups-cache-key",
		Name:       "auto-cache",
		Group:      "auto",
		AutoGroups: `["vip","default"]`,
	}

	require.NoError(t, cacheSetTokenForTest(token))
	cached, err := cache.CacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, token.AutoGroups, cached.AutoGroups)
	var groups []string
	err = common.UnmarshalJsonStr(cached.AutoGroups, &groups)
	require.NoError(t, err)
	assert.Equal(t, []string{"vip", "default"}, groups)
}

func TestTokenUpdateSynchronouslyNarrowsPreheatedAutoGroupsCache(t *testing.T) {
	cachefixture.Open(t)
	cachefixture.NewRedis(t)
	token := record.Token{
		UserId:          7,
		Key:             "token-auto-groups-update-cache-key",
		Name:            "auto-cache-update",
		Status:          common.TokenStatusEnabled,
		ExpiredTime:     -1,
		UnlimitedQuota:  true,
		Group:           "auto",
		CrossGroupRetry: true,
		AutoGroups:      `["default","vip"]`,
	}
	require.NoError(t, apikeystorage.Persistence().Insert(&token))
	require.NoError(t, cacheSetTokenForTest(token))

	preheated, err := cache.CacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.JSONEq(t, `["default","vip"]`, preheated.AutoGroups)

	domainToken := record.ToDomain(&token)
	store := repository.NewStore(apikeystorage.Persistence(), func(key string) (*domain.Token, error) {
		loaded, err := apikeystorage.Reader().Read(key, false)
		return record.ToDomain(loaded), err
	})
	require.NoError(t, store.SetAutoGroups(domainToken, []string{"vip"}))
	token.AutoGroups = domainToken.AutoGroups
	require.NoError(t, apikeystorage.Persistence().Update(&token))
	// Update narrows access: invalidate the old hash and fence stale snapshots before the database write.
	_, cacheErr := cache.CacheGetTokenByKey(token.Key)
	require.Error(t, cacheErr, "the pre-update cache entry must be invalidated")
	reloaded, err := apikeystorage.Reader().Read(token.Key, false)
	require.NoError(t, err)
	assert.JSONEq(t, `["vip"]`, reloaded.AutoGroups)
}

// cacheSetTokenForTest publishes a complete fixture snapshot, including quota fields.
func cacheSetTokenForTest(token record.Token) error {
	_, err := cache.CacheInitToken(token)
	return err
}
