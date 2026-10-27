package identity_test

import (
	"context"
	"errors"
	"fmt"
	cacheaccounts "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	accountscore "github.com/QuantumNous/new-api/internal/app/accountscore"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	cachefixture "github.com/QuantumNous/new-api/internal/contracttest/cachefixture"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	model "github.com/QuantumNous/new-api/model"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type setMiniRedisTimeOnEvalHook struct {
	server *miniredis.Miniredis
	at     time.Time
}

func (hook setMiniRedisTimeOnEvalHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "eval" {
		hook.server.SetTime(hook.at)
	}
	return ctx, nil
}

func (setMiniRedisTimeOnEvalHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (setMiniRedisTimeOnEvalHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (setMiniRedisTimeOnEvalHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func setupUserSessionTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &identityrecord.UserSession{}))
	require.NoError(t, model.DB.Exec("DELETE FROM user_sessions").Error)
	oldRedisEnabled := common.RedisEnabled
	oldActiveLimit := common.UserSessionActiveLimit
	oldIssuanceLimit := common.UserSessionIssuanceLimit
	oldIssuanceWindow := common.UserSessionIssuanceWindowSeconds
	oldRevokedRetention := common.UserSessionRevokedRetentionDays
	common.RedisEnabled = false
	common.UserSessionActiveLimit = common.DefaultUserSessionActiveLimit
	common.UserSessionIssuanceLimit = common.DefaultUserSessionIssuanceLimit
	common.UserSessionIssuanceWindowSeconds = int64(common.DefaultUserSessionIssuanceWindowSeconds)
	common.UserSessionRevokedRetentionDays = common.DefaultUserSessionRevokedRetentionDays
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.UserSessionActiveLimit = oldActiveLimit
		common.UserSessionIssuanceLimit = oldIssuanceLimit
		common.UserSessionIssuanceWindowSeconds = oldIssuanceWindow
		common.UserSessionRevokedRetentionDays = oldRevokedRetention
	})
}

func createUserSessionTestUser(t *testing.T, userID int, authVersion int64) {
	t.Helper()
	user := model.User{
		Id:          userID,
		Username:    fmt.Sprintf("user-session-%d", userID),
		Password:    "unused",
		Status:      common.UserStatusEnabled,
		Role:        common.RoleCommonUser,
		Group:       "default",
		AffCode:     fmt.Sprintf("session-aff-%d", userID),
		AuthVersion: authVersion,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	t.Cleanup(func() { _ = model.DB.Unscoped().Delete(&model.User{}, userID).Error })
}

func newTestUserSession(sid string, userID int, now int64) *identityrecord.UserSession {
	return &identityrecord.UserSession{
		SID:             sid,
		UserID:          userID,
		Version:         1,
		UserAuthVersion: 1,
		Status:          identityrecord.UserSessionStatusActive,
		RefreshHash:     fmt.Sprintf("current-%s", sid),
		LoginMethod:     "password",
		IP:              "127.0.0.1",
		UserAgent:       "model-test",
		CreatedAt:       now,
		LastActiveAt:    now,
		ExpiresAt:       now + int64((30*24*time.Hour)/time.Second),
	}
}

func TestUserSessionCacheTTLUsesShortCacheWindow(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	server := cachefixture.NewRedis(t)
	now := time.Now().Unix()
	tests := []struct {
		name       string
		status     string
		expiresAt  int64
		wantMaxTTL time.Duration
	}{
		{name: "active", status: identityrecord.UserSessionStatusActive, expiresAt: now + 300, wantMaxTTL: 2 * time.Second},
		{name: "revoking", status: identityrecord.UserSessionStatusRevoking, expiresAt: now + 300, wantMaxTTL: 2 * time.Second},
		{name: "revoked", status: identityrecord.UserSessionStatusRevoked, expiresAt: now + 300, wantMaxTTL: 2 * time.Second},
		{name: "already expired", status: identityrecord.UserSessionStatusRevoked, expiresAt: now - 1, wantMaxTTL: time.Second},
	}

	for index, test := range tests {
		sid := fmt.Sprintf("short-cache-ttl-%d", index)
		entry := identitystorage.SQL().TestCacheEntry(newTestUserSession(sid, 1100+index, now))
		entry.Status = test.status
		entry.ExpiresAt = test.expiresAt
		if test.status != identityrecord.UserSessionStatusActive {
			entry.RevokedAt = now
		}

		cacheDeadline := time.Time{}
		if test.status == identityrecord.UserSessionStatusActive {
			cacheDeadline = identitystorage.SQL().TestUserSessionCacheDeadline()
		}
		require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(entry, cacheDeadline), test.name)
		ttl := server.TTL(identitystorage.SQL().TestUserSessionCacheKey(sid))
		assert.Positive(t, ttl, test.name)
		assert.LessOrEqual(t, ttl, test.wantMaxTTL, test.name)
	}

	initialTTL := server.TTL(identitystorage.SQL().TestUserSessionCacheKey("short-cache-ttl-0"))
	server.FastForward(time.Second)
	_, err := identitystorage.SQL().TestGetUserSessionCache("short-cache-ttl-0")
	require.NoError(t, err)
	remainingTTL := server.TTL(identitystorage.SQL().TestUserSessionCacheKey("short-cache-ttl-0"))
	assert.Positive(t, remainingTTL)
	assert.LessOrEqual(t, remainingTTL, initialTTL-time.Second, "cache reads must not renew the bounded TTL")

	common.SyncFrequency = 10
	nearExpiry := identitystorage.SQL().TestCacheEntry(newTestUserSession("short-cache-ttl-near-expiry", 1199, now))
	nearExpiry.ExpiresAt = time.Now().Add(2 * time.Second).Unix()
	nearExpiryDeadline := identitystorage.SQL().TestUserSessionCacheDeadline()
	remainingLifetime := time.Until(time.Unix(nearExpiry.ExpiresAt, 0))
	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(nearExpiry, nearExpiryDeadline))
	nearExpiryTTL := server.TTL(identitystorage.SQL().TestUserSessionCacheKey(nearExpiry.SID))
	assert.Positive(t, nearExpiryTTL)
	assert.LessOrEqual(t, nearExpiryTTL, remainingLifetime, "cache TTL must not exceed the Session remaining lifetime")

	common.SyncFrequency = 0
	fallback := identitystorage.SQL().TestCacheEntry(newTestUserSession("short-cache-ttl-fallback", 1200, now))
	fallback.ExpiresAt = now + 300
	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(fallback, identitystorage.SQL().TestUserSessionCacheDeadline()))
	fallbackTTL := server.TTL(identitystorage.SQL().TestUserSessionCacheKey(fallback.SID))
	assert.Greater(t, fallbackTTL, 59*time.Second)
	assert.LessOrEqual(t, fallbackTTL, 60*time.Second, "non-positive cache frequency must use the existing 60-second fallback")
}

func TestStaleActiveSessionCacheFillCannotRestartWindowAfterDenyExpires(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	server := cachefixture.NewRedis(t)
	now := time.Now().Unix()
	active := identitystorage.SQL().TestCacheEntry(newTestUserSession("stale-active-cache-fill", 1201, now))
	denied := *active
	denied.Status = identityrecord.UserSessionStatusRevoked
	denied.RevokedAt = now
	denied.RevokedReason = "test-revoke"

	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(&denied, time.Time{}))
	cacheKey := identitystorage.SQL().TestUserSessionCacheKey(active.SID)
	assert.True(t, server.Exists(cacheKey))
	server.FastForward(3 * time.Second)
	assert.False(t, server.Exists(cacheKey), "the short deny tombstone must have expired in this race setup")

	err := identitystorage.SQL().TestWriteUserSessionCache(active, time.Now().Add(-time.Millisecond))
	assert.ErrorIs(t, err, identitystore.TestErrUserSessionCacheObservationStale)
	assert.False(t, server.Exists(cacheKey), "a delayed pre-revoke active snapshot must not restart a fresh cache window")
}

func TestActiveSessionCacheFillUsesRemainingObservationWindow(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	server := cachefixture.NewRedis(t)
	now := time.Now().Unix()
	entry := identitystorage.SQL().TestCacheEntry(newTestUserSession("bounded-active-cache-fill", 1202, now))
	deadline := time.Now().Add(1500 * time.Millisecond)

	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(entry, deadline))
	ttl := server.TTL(identitystorage.SQL().TestUserSessionCacheKey(entry.SID))
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, 1500*time.Millisecond, "a delayed fill must inherit only the unused observation window")
}

func TestSessionCacheLuaUsesAbsoluteActiveAndRelativeDenyExpiry(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	server := cachefixture.NewRedis(t)
	now := time.Now().Unix()
	deadline := time.Now().Add(10 * time.Second)
	common.RDB.AddHook(setMiniRedisTimeOnEvalHook{server: server, at: deadline.Add(time.Second)})

	active := identitystorage.SQL().TestCacheEntry(newTestUserSession("delayed-active-cache-eval", 1203, now))
	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(active, deadline))
	assert.False(t, server.Exists(identitystorage.SQL().TestUserSessionCacheKey(active.SID)), "an active fill executed after its absolute deadline must not recreate the cache")

	denied := identitystorage.SQL().TestCacheEntry(newTestUserSession("delayed-deny-cache-eval", 1204, now))
	denied.Status = identityrecord.UserSessionStatusRevoked
	denied.RevokedAt = now
	denied.RevokedReason = "test-revoke"
	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(denied, time.Time{}))
	denyTTL := server.TTL(identitystorage.SQL().TestUserSessionCacheKey(denied.SID))
	assert.Positive(t, denyTTL)
	assert.LessOrEqual(t, denyTTL, 2*time.Second, "a delayed deny publication must receive a full relative short TTL at Redis execution")
}

func TestUserSessionCreateListAndRevokeOne(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	user := model.User{Id: 1001, Username: "session-list-user", Password: "password", AuthVersion: 1}
	require.NoError(t, model.DB.Create(&user).Error)
	t.Cleanup(func() { _ = model.DB.Unscoped().Delete(&model.User{}, user.Id).Error })
	first := newTestUserSession("session-one", 1001, now)
	second := newTestUserSession("session-two", 1001, now+1)
	require.NoError(t, identitystorage.SQL().CreateUserSession(first))
	require.NoError(t, identitystorage.SQL().CreateUserSession(second))

	sessions, err := identitystorage.SQL().ListActiveUserSessions(1001, first.SID, now)
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, first.SID, sessions[0].SID)

	revoked, err := identitystorage.SQL().RevokeUserSession(1001, first.SID, "user_revoked")
	require.NoError(t, err)
	assert.True(t, revoked)
	revoked, err = identitystorage.SQL().RevokeUserSession(1001, first.SID, "duplicate")
	require.NoError(t, err)
	assert.False(t, revoked)

	_, err = identitystorage.SQL().GetUserSessionCached(first.SID)
	assert.ErrorIs(t, err, identitydomain.ErrSessionInactive)
	active, err := identitystorage.SQL().GetUserSessionCached(second.SID)
	require.NoError(t, err)
	assert.Equal(t, second.SID, active.SID)
}

func TestRotateUserSessionRefreshRaceAndReuse(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1002, 1)
	session := newTestUserSession("rotate-session", 1002, now)
	require.NoError(t, identitystorage.SQL().CreateUserSession(session))

	rotated, err := identitystorage.SQL().RotateUserSessionRefresh(1002, session.SID, session.RefreshHash, "next-hash", now+10, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "next-hash", rotated.RefreshHash)
	assert.Equal(t, session.RefreshHash, rotated.PreviousRefreshHash)
	assert.Equal(t, now+40, rotated.PreviousValidUntil)

	_, err = identitystorage.SQL().RotateUserSessionRefresh(1002, session.SID, session.RefreshHash, "unused-hash", now+20, 30*time.Second)
	assert.ErrorIs(t, err, identitydomain.ErrSessionRefreshRace)
	_, err = identitystorage.SQL().RotateUserSessionRefresh(1002, session.SID, "unknown-hash", "unused-hash", now+20, 30*time.Second)
	assert.ErrorIs(t, err, identitydomain.ErrSessionRefreshInvalid)
	stored, getErr := identitystorage.SQL().GetUserSessionBySID(session.SID)
	require.NoError(t, getErr)
	assert.Equal(t, identityrecord.UserSessionStatusActive, stored.Status)

	_, err = identitystorage.SQL().RotateUserSessionRefresh(1002, session.SID, session.RefreshHash, "unused-hash", now+41, 30*time.Second)
	assert.ErrorIs(t, err, identitydomain.ErrSessionRefreshReuse)
	stored, getErr = identitystorage.SQL().GetUserSessionBySID(session.SID)
	require.NoError(t, getErr)
	assert.Equal(t, identityrecord.UserSessionStatusRevoked, stored.Status)
	assert.Equal(t, "refresh_reuse", stored.RevokedReason)
}

func TestUserSessionPreviousRefreshHashNormalizesLegacyPadding(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1010, 1)
	digest := strings.Repeat("a", 64)

	blank := newTestUserSession("legacy-blank-previous-hash", 1010, now)
	blank.PreviousRefreshHash = strings.Repeat(" ", 64)
	blank.PreviousValidUntil = now + 60
	require.NoError(t, model.DB.Create(blank).Error)
	loadedBlank, err := identitystorage.SQL().GetUserSessionBySID(blank.SID)
	require.NoError(t, err)
	assert.Empty(t, loadedBlank.PreviousRefreshHash)

	valid := newTestUserSession("legacy-valid-previous-hash", 1010, now)
	valid.RefreshHash = strings.Repeat("b", 64)
	valid.PreviousRefreshHash = digest
	valid.PreviousValidUntil = now + 60
	require.NoError(t, model.DB.Create(valid).Error)
	loadedValid, err := identitystorage.SQL().GetUserSessionBySID(valid.SID)
	require.NoError(t, err)
	assert.Equal(t, digest, loadedValid.PreviousRefreshHash)

	require.NoError(t, model.DB.Model(&identityrecord.UserSession{}).Where("sid = ?", valid.SID).
		Updates(map[string]any{
			"previous_refresh_hash": digest + "   ",
			"previous_valid_until":  now + 60,
		}).Error)
	_, err = identitystorage.SQL().RotateUserSessionRefresh(valid.UserID, valid.SID, digest, strings.Repeat("c", 64), now+1, 30*time.Second)
	assert.ErrorIs(t, err, identitydomain.ErrSessionRefreshRace)

	revoked, err := identitystorage.SQL().RevokeUserSessionByRefreshHash(valid.SID, digest, "legacy-padded-refresh-logout")
	require.NoError(t, err)
	assert.True(t, revoked, "refresh-cookie logout must accept a legacy CHAR-padded previous digest inside its grace window")
}

func TestUserSessionCacheExcludesRefreshDigests(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	cachefixture.NewRedis(t)
	now := time.Now().Unix()
	session := newTestUserSession("cache-without-refresh-digests", 1011, now)
	session.PreviousRefreshHash = strings.Repeat("a", 64)
	session.PreviousValidUntil = now + 30
	require.NoError(t, identitystorage.SQL().TestWriteUserSessionCache(identitystorage.SQL().TestCacheEntry(session), identitystorage.SQL().TestUserSessionCacheDeadline()))

	cacheKey := identitystorage.SQL().TestUserSessionCacheKey(session.SID)
	fields, err := common.RDB.HGetAll(context.Background(), cacheKey).Result()
	require.NoError(t, err)
	assert.NotContains(t, fields, "RefreshHash")
	assert.NotContains(t, fields, "PreviousRefreshHash")
	assert.NotContains(t, fields, "PreviousValidUntil")

	require.NoError(t, common.RDB.HSet(context.Background(), cacheKey,
		"RefreshHash", strings.Repeat("b", 64),
		"PreviousRefreshHash", strings.Repeat("c", 64)+"   ",
		"PreviousValidUntil", now+30,
	).Err())
	entry, err := identitystorage.SQL().TestGetUserSessionCache(session.SID)
	require.NoError(t, err)
	cachedSession := identitystore.SessionForTest(entry)
	assert.Empty(t, cachedSession.RefreshHash)
	assert.Empty(t, cachedSession.PreviousRefreshHash)
	assert.Zero(t, cachedSession.PreviousValidUntil)
}

func TestRevokeOtherUserSessionsKeepsCurrent(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1003, 1)
	createUserSessionTestUser(t, 1004, 1)
	for _, sid := range []string{"current-session", "other-one", "other-two"} {
		session := newTestUserSession(sid, 1003, now)
		if sid == "other-one" {
			session.UserAuthVersion = 99
		}
		require.NoError(t, identitystorage.SQL().CreateUserSession(session))
	}
	require.NoError(t, identitystorage.SQL().CreateUserSession(newTestUserSession("different-user", 1004, now)))

	count, err := identitystorage.SQL().RevokeOtherUserSessions(1003, "current-session", "revoke_others")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	current, err := identitystorage.SQL().GetUserSessionCached("current-session")
	require.NoError(t, err)
	assert.Equal(t, identityrecord.UserSessionStatusActive, current.Status)
	_, err = identitystorage.SQL().GetUserSessionCached("other-one")
	assert.True(t, errors.Is(err, identitydomain.ErrSessionInactive))
	stale, err := identitystorage.SQL().GetUserSessionBySID("other-one")
	require.NoError(t, err)
	assert.Equal(t, identityrecord.UserSessionStatusRevoked, stale.Status, "revocation must include active sessions from stale auth versions")
	different, err := identitystorage.SQL().GetUserSessionCached("different-user")
	require.NoError(t, err)
	assert.Equal(t, 1004, different.UserID)
}

func TestRevokeUserSessionByRefreshHashRequiresSecret(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1005, 1)
	session := newTestUserSession("refresh-logout-session", 1005, now)
	require.NoError(t, identitystorage.SQL().CreateUserSession(session))

	revoked, err := identitystorage.SQL().RevokeUserSessionByRefreshHash(session.SID, "wrong-hash", "logout")
	require.NoError(t, err)
	assert.False(t, revoked)
	active, err := identitystorage.SQL().GetUserSessionCached(session.SID)
	require.NoError(t, err)
	assert.Equal(t, identityrecord.UserSessionStatusActive, active.Status)

	revoked, err = identitystorage.SQL().RevokeUserSessionByRefreshHash(session.SID, session.RefreshHash, "logout")
	require.NoError(t, err)
	assert.True(t, revoked)
	_, err = identitystorage.SQL().GetUserSessionCached(session.SID)
	assert.ErrorIs(t, err, identitydomain.ErrSessionInactive)
}

func TestUserSessionGrowthCountsUseBroadActiveAndStrictIssuancePredicates(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1006, 7)
	rows := []identityrecord.UserSession{
		*newTestUserSession("count-current-version", 1006, now-10),
		*newTestUserSession("count-stale-version", 1006, now-9),
		*newTestUserSession("count-expired", 1006, now-8),
		*newTestUserSession("count-revoked", 1006, now-7),
		*newTestUserSession("count-cutoff", 1006, now-3600),
	}
	rows[0].UserAuthVersion = 7
	rows[1].UserAuthVersion = 2
	rows[2].UserAuthVersion = 7
	rows[2].ExpiresAt = now
	rows[3].UserAuthVersion = 7
	rows[3].Status = identityrecord.UserSessionStatusRevoked
	rows[3].RevokedAt = now - 1
	rows[4].UserAuthVersion = 7
	rows[4].CreatedAt = now - 3600
	rows[4].ExpiresAt = now
	require.NoError(t, model.DB.Create(&rows).Error)

	activeCount, err := identitystorage.SQL().CountActiveUserSessions(1006, now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), activeCount, "active count includes stale auth versions but excludes expired and revoked rows")

	issuedCount, err := identitystorage.SQL().CountUserSessionsCreatedSince(1006, now-3600)
	require.NoError(t, err)
	assert.Equal(t, int64(4), issuedCount, "issuance count includes every status and uses a strict cutoff")
	globalCount, err := identitystorage.SQL().CountUserSessionsCreatedSince(0, now-3600)
	require.NoError(t, err)
	assert.Equal(t, issuedCount, globalCount)
}

func TestListActiveUserSessionsKeepsCurrentAndBoundsOtherSessions(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1007, 7)
	current := newTestUserSession("list-current", 1007, now-1000)
	current.UserAuthVersion = 7
	rows := make([]identityrecord.UserSession, 0, 107)
	rows = append(rows, *current)
	for i := range 105 {
		session := newTestUserSession(fmt.Sprintf("list-other-%03d", i), 1007, now-int64(i))
		session.UserAuthVersion = 7
		rows = append(rows, *session)
	}
	stale := newTestUserSession("list-stale-auth-version", 1007, now+1)
	stale.UserAuthVersion = 6
	rows = append(rows, *stale)
	require.NoError(t, model.DB.CreateInBatches(rows, 100).Error)

	sessions, err := identitystorage.SQL().ListActiveUserSessions(1007, current.SID, now)
	require.NoError(t, err)
	require.Len(t, sessions, 100)
	assert.Equal(t, current.SID, sessions[0].SID)
	for _, session := range sessions {
		assert.Equal(t, int64(7), session.UserAuthVersion)
		assert.NotEqual(t, stale.SID, session.SID)
	}

	sessionsWithoutCurrent, err := identitystorage.SQL().ListActiveUserSessions(1007, "missing-current", now)
	require.NoError(t, err)
	assert.Len(t, sessionsWithoutCurrent, identitystore.TestUserSessionListLimit, "a missing current SID must not reduce the total list limit")
}

func TestRevokeUserSessionsReturnsCumulativeProgressAndSupportsRetry(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	createUserSessionTestUser(t, 1008, 1)
	rows := make([]identityrecord.UserSession, 0, identitystore.TestUserSessionRevokeBatchSize+1)
	for i := range identitystore.TestUserSessionRevokeBatchSize + 1 {
		rows = append(rows, *newTestUserSession(fmt.Sprintf("batch-revoke-%03d", i), 1008, now))
	}
	require.NoError(t, model.DB.CreateInBatches(rows, 100).Error)

	forcedErr := errors.New("forced second revoke batch failure")
	callbackName := "test:fail_second_user_session_revoke_batch"
	updateCalls := 0
	callbackRegistered := true
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "user_sessions" {
			updateCalls++
			if updateCalls == 2 {
				tx.AddError(forcedErr)
			}
		}
	}))
	t.Cleanup(func() {
		if callbackRegistered {
			_ = model.DB.Callback().Update().Remove(callbackName)
		}
	})

	affected, err := identitystorage.SQL().RevokeAllUserSessions(1008, "batch-test")
	assert.ErrorIs(t, err, forcedErr)
	assert.Equal(t, int64(identitystore.TestUserSessionRevokeBatchSize), affected)
	require.NoError(t, model.DB.Callback().Update().Remove(callbackName))
	callbackRegistered = false

	retried, err := identitystorage.SQL().RevokeAllUserSessions(1008, "batch-test-retry")
	require.NoError(t, err)
	assert.Equal(t, int64(1), retried)
	var activeCount int64
	require.NoError(t, model.DB.Model(&identityrecord.UserSession{}).Where("user_id = ? AND status = ?", 1008, identityrecord.UserSessionStatusActive).Count(&activeCount).Error)
	assert.Zero(t, activeCount)
}

func TestDeleteExpiredUserSessionsLoopsInChunksAndRechecksPredicate(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	common.UserSessionRevokedRetentionDays = 7
	common.UserSessionIssuanceWindowSeconds = 3600
	oldCreatedAt := now - 7200
	rows := make([]identityrecord.UserSession, 0, identitystore.TestUserSessionCleanupScanLimit+5)
	race := newTestUserSession("cleanup-race", 1009, now-1000)
	race.CreatedAt = oldCreatedAt
	race.ExpiresAt = now - 1000
	rows = append(rows, *race)
	for i := range identitystore.TestUserSessionCleanupScanLimit + 1 {
		session := newTestUserSession(fmt.Sprintf("cleanup-expired-%04d", i), 1009, now-100)
		session.CreatedAt = oldCreatedAt - int64(i)
		session.ExpiresAt = now - 100
		rows = append(rows, *session)
	}
	oldRevoked := newTestUserSession("cleanup-old-revoked", 1009, now-10)
	oldRevoked.CreatedAt = oldCreatedAt
	oldRevoked.Status = identityrecord.UserSessionStatusRevoked
	oldRevoked.RevokedAt = now - int64(8*24*time.Hour/time.Second)
	rows = append(rows, *oldRevoked)
	recentRevoked := newTestUserSession("cleanup-recent-revoked", 1009, now-9)
	recentRevoked.CreatedAt = oldCreatedAt
	recentRevoked.Status = identityrecord.UserSessionStatusRevoked
	recentRevoked.RevokedAt = now - int64(6*24*time.Hour/time.Second)
	recentRevoked.ExpiresAt = now - 100
	rows = append(rows, *recentRevoked)
	recentIssuedExpired := newTestUserSession("cleanup-recent-issued-expired", 1009, now-1800)
	recentIssuedExpired.ExpiresAt = now - 100
	rows = append(rows, *recentIssuedExpired)
	expiryBoundary := newTestUserSession("cleanup-expiry-boundary", 1009, now-7)
	expiryBoundary.CreatedAt = oldCreatedAt
	expiryBoundary.ExpiresAt = now
	rows = append(rows, *expiryBoundary)
	revokedBoundary := newTestUserSession("cleanup-revoked-boundary", 1009, now-6)
	revokedBoundary.CreatedAt = oldCreatedAt
	revokedBoundary.Status = identityrecord.UserSessionStatusRevoked
	revokedBoundary.RevokedAt = now - int64(7*24*time.Hour/time.Second)
	rows = append(rows, *revokedBoundary)
	live := newTestUserSession("cleanup-live", 1009, now-8)
	rows = append(rows, *live)
	require.NoError(t, model.DB.CreateInBatches(rows, 100).Error)

	callbackName := "test:recheck_user_session_cleanup_predicate"
	deleteCalls := 0
	mutated := false
	require.NoError(t, model.DB.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "user_sessions" {
			return
		}
		deleteCalls++
		if !mutated {
			mutated = true
			tx.Exec("UPDATE user_sessions SET expires_at = ? WHERE sid = ?", now+3600, race.SID)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Delete().Remove(callbackName) })

	require.NoError(t, identitystorage.SQL().DeleteExpiredUserSessions(now))
	require.NoError(t, identitystorage.SQL().DeleteOldRevokedUserSessions(now))
	assert.Equal(t, 4, deleteCalls, "expired and retained-revoked scans each delete in bounded chunks")
	var remaining []identityrecord.UserSession
	require.NoError(t, model.DB.Order("sid").Find(&remaining).Error)
	require.Len(t, remaining, 6)
	remainingSIDs := make([]string, 0, len(remaining))
	for _, session := range remaining {
		remainingSIDs = append(remainingSIDs, session.SID)
	}
	assert.ElementsMatch(t, []string{
		race.SID,
		recentRevoked.SID,
		recentIssuedExpired.SID,
		expiryBoundary.SID,
		revokedBoundary.SID,
		live.SID,
	}, remainingSIDs)
}

func TestUserSessionGrowthQueryIndexesExist(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	migrator := model.DB.Migrator()
	assert.True(t, migrator.HasIndex(&identityrecord.UserSession{}, "idx_user_sessions_expires_at"))
	assert.True(t, migrator.HasIndex(&identityrecord.UserSession{}, "idx_user_sessions_user_created"))
	assert.True(t, migrator.HasIndex(&identityrecord.UserSession{}, "idx_user_sessions_status_revoked"))
}

func TestUserBaseIncludesAuthorizationFields(t *testing.T) {
	nativeFixture(t)

	user := model.User{
		Id:          42,
		Username:    "cache-user",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "vip",
		Quota:       123,
		AuthVersion: 7,
	}
	base := cacheaccounts.ToBaseUser(&user)
	assert.Equal(t, user.Role, base.Role)
	assert.Equal(t, user.AuthVersion, base.AuthVersion)
	assert.Equal(t, 3, base.CacheSchema)
	assert.Equal(t, user.Quota, base.Quota)
}

func TestUserUpdateBumpsAuthVersionOnlyForAuthorizationChanges(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	user := &model.User{
		Username: "auth-version-user",
		Password: "hashed-placeholder",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() { _ = model.DB.Unscoped().Delete(&model.User{}, user.Id).Error })
	assert.Equal(t, int64(1), user.AuthVersion)

	user.DisplayName = "profile-only"
	require.NoError(t, accountscore.Update(user, false))
	assert.Equal(t, int64(1), user.AuthVersion)

	user.Group = "vip"
	require.NoError(t, accountscore.Update(user, false))
	assert.Equal(t, int64(2), user.AuthVersion)

	user.Role = common.RoleAdminUser
	require.NoError(t, accountscore.Update(user, false))
	assert.Equal(t, int64(3), user.AuthVersion)
}

func TestPasswordResetBumpsAuthVersionAndRevokesSessions(t *testing.T) {
	nativeFixture(t)

	setupUserSessionTest(t)
	now := time.Now().Unix()
	user := &model.User{
		Username: "password-reset-user",
		Password: "old-hash",
		Email:    "password-reset@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, model.DB.Create(user).Error)
	t.Cleanup(func() { _ = model.DB.Unscoped().Delete(&model.User{}, user.Id).Error })
	session := newTestUserSession("password-reset-session", user.Id, now)
	require.NoError(t, identitystorage.SQL().CreateUserSession(session))

	require.NoError(t, accountscore.ResetUserPasswordByEmail(user.Email, "new-password"))
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, int64(2), stored.AuthVersion)
	storedSession, err := identitystorage.SQL().GetUserSessionBySID(session.SID)
	require.NoError(t, err)
	assert.Equal(t, identityrecord.UserSessionStatusRevoked, storedSession.Status)
	assert.Equal(t, "password_reset", storedSession.RevokedReason)
}
