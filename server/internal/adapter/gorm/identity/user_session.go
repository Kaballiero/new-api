package identity

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	locking "github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"gorm.io/gorm"
	"time"
)

const (
	userSessionCacheSchema      = 1
	userSessionListLimit        = 100
	userSessionRevokeBatchSize  = 500
	userSessionCleanupScanLimit = 1000
	userSessionCleanupBatchSize = 500
)

var errUserSessionCacheObservationStale = errors.New("user session cache observation is stale")

type userSessionCacheEntry struct {
	SID             string
	UserID          int
	Version         int64
	UserAuthVersion int64
	Status          string
	LoginMethod     string
	IP              string
	UserAgent       string
	CreatedAt       int64
	LastActiveAt    int64
	ExpiresAt       int64
	RevokedAt       int64
	RevokedReason   string
	CacheSchema     int
}

func (s Store) cacheEntry(session *identityrecord.UserSession) *userSessionCacheEntry {
	return &userSessionCacheEntry{
		SID:             session.SID,
		UserID:          session.UserID,
		Version:         session.Version,
		UserAuthVersion: session.UserAuthVersion,
		Status:          session.Status,
		LoginMethod:     session.LoginMethod,
		IP:              session.IP,
		UserAgent:       session.UserAgent,
		CreatedAt:       session.CreatedAt,
		LastActiveAt:    session.LastActiveAt,
		ExpiresAt:       session.ExpiresAt,
		RevokedAt:       session.RevokedAt,
		RevokedReason:   session.RevokedReason,
		CacheSchema:     userSessionCacheSchema,
	}
}

func (entry *userSessionCacheEntry) session() *identityrecord.UserSession {
	return &identityrecord.UserSession{
		SID:             entry.SID,
		UserID:          entry.UserID,
		Version:         entry.Version,
		UserAuthVersion: entry.UserAuthVersion,
		Status:          entry.Status,
		LoginMethod:     entry.LoginMethod,
		IP:              entry.IP,
		UserAgent:       entry.UserAgent,
		CreatedAt:       entry.CreatedAt,
		LastActiveAt:    entry.LastActiveAt,
		ExpiresAt:       entry.ExpiresAt,
		RevokedAt:       entry.RevokedAt,
		RevokedReason:   entry.RevokedReason,
	}
}

func (s Store) userSessionCacheKey(sid string) string {
	digest := common.GenerateHMACWithKey([]byte("user-session-cache-v1:"+common.SessionSecret), sid)
	return "auth:session:" + digest
}

func (s Store) userSessionCacheDeadline() time.Time {
	return time.Now().Add(time.Duration(s.env.UserCacheTTLSeconds()) * time.Second)
}

func (s Store) CreateUserSession(session *identityrecord.UserSession) error {
	cacheDeadline := s.userSessionCacheDeadline()
	if err := s.createUserSessionWithTx(s.env.DB(), session); err != nil {
		return err
	}
	return s.publishCreatedUserSession(session, cacheDeadline)
}

func (s Store) createUserSessionWithTx(tx *gorm.DB, session *identityrecord.UserSession) error {
	now := time.Now().Unix()
	if session == nil || session.SID == "" || session.UserID <= 0 || session.UserAuthVersion <= 0 || session.RefreshHash == "" || session.ExpiresAt <= now {
		return identitydomain.ErrSessionInvalid
	}
	if session.Version <= 0 {
		session.Version = 1
	}
	if session.Status == "" {
		session.Status = identityrecord.UserSessionStatusActive
	}
	if session.Status != identityrecord.UserSessionStatusActive || session.RevokedAt != 0 {
		return identitydomain.ErrSessionInvalid
	}
	if session.LastActiveAt == 0 {
		session.LastActiveAt = now
	}
	if session.CreatedAt == 0 {
		session.CreatedAt = now
	}
	return tx.Create(session).Error
}

func (s Store) publishCreatedUserSession(session *identityrecord.UserSession, cacheDeadline time.Time) error {
	if err := s.writeUserSessionCache(s.cacheEntry(session), cacheDeadline); err != nil {
		if errors.Is(err, errUserSessionCacheObservationStale) {
			return s.confirmUserSessionActiveSnapshot(session)
		}
		if errors.Is(err, identitydomain.ErrSessionInactive) {
			return err
		}
		common.SysLog("failed to populate newly created user session cache: " + err.Error())
	}
	return nil
}

func (s Store) CountActiveUserSessions(userID int, now int64) (int64, error) {
	if userID <= 0 {
		return 0, identitydomain.ErrSessionInvalid
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	var count int64
	err := s.env.DB().Model(&identityrecord.UserSession{}).
		Where("user_id = ? AND status = ? AND expires_at > ?", userID, identityrecord.UserSessionStatusActive, now).
		Count(&count).Error
	return count, err
}

// CountUserSessionsCreatedSince counts every issued row, regardless of its
// current status or expiry. userID zero selects the global count.
func (s Store) CountUserSessionsCreatedSince(userID int, createdAfter int64) (int64, error) {
	if userID < 0 || createdAfter <= 0 {
		return 0, identitydomain.ErrSessionInvalid
	}
	query := s.env.DB().Model(&identityrecord.UserSession{}).Where("created_at > ?", createdAfter)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	var count int64
	err := query.Count(&count).Error
	return count, err
}

func (s Store) GetUserSessionBySID(sid string) (*identityrecord.UserSession, error) {
	if sid == "" {
		return nil, identitydomain.ErrSessionInvalid
	}
	var session identityrecord.UserSession
	if err := s.env.DB().Where("sid = ?", sid).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// GetUserSessionCached validates cached state first and falls back to the
// database on a miss or Redis read failure. A deny tombstone never falls back.
func (s Store) GetUserSessionCached(sid string) (*identityrecord.UserSession, error) {
	if sid == "" {
		return nil, identitydomain.ErrSessionInvalid
	}
	if common.RedisEnabled {
		entry, err := s.getUserSessionCache(sid)
		if err == nil {
			return entry.session(), nil
		}
		if errors.Is(err, identitydomain.ErrSessionInactive) {
			return nil, err
		}
	}

	cacheDeadline := s.userSessionCacheDeadline()
	session, err := s.GetUserSessionBySID(sid)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if session.Status != identityrecord.UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= now {
		if common.RedisEnabled {
			entry := s.cacheEntry(session)
			entry.Status = identityrecord.UserSessionStatusRevoked
			_ = s.writeUserSessionCache(entry, time.Time{})
		}
		return nil, identitydomain.ErrSessionInactive
	}
	if common.RedisEnabled {
		if err := s.writeUserSessionCache(s.cacheEntry(session), cacheDeadline); err != nil {
			if errors.Is(err, errUserSessionCacheObservationStale) {
				if confirmErr := s.confirmUserSessionActiveSnapshot(session); confirmErr != nil {
					return nil, confirmErr
				}
				return session, nil
			}
			if errors.Is(err, identitydomain.ErrSessionInactive) {
				return nil, err
			}
			common.SysLog("failed to synchronously populate user session cache: " + err.Error())
		}
	}
	return session, nil
}

func (s Store) getUserSessionCache(sid string) (*userSessionCacheEntry, error) {
	var entry userSessionCacheEntry
	if err := common.RedisHGetObj(s.userSessionCacheKey(sid), &entry); err != nil {
		return nil, err
	}
	if entry.CacheSchema != userSessionCacheSchema || entry.SID != sid || entry.UserID <= 0 || entry.Version <= 0 || entry.UserAuthVersion <= 0 {
		return nil, fmt.Errorf("user session cache schema is stale")
	}
	if entry.Status != identityrecord.UserSessionStatusActive || entry.RevokedAt != 0 || entry.ExpiresAt <= time.Now().Unix() {
		return nil, identitydomain.ErrSessionInactive
	}
	return &entry, nil
}

// writeUserSessionCache writes a bounded Session snapshot. Active snapshots
// must carry a deadline captured immediately before their authoritative
// database read or mutation. Delayed fills inherit the unspent portion of that
// window, so a stale active snapshot cannot outlive a short deny tombstone and
// reactivate a revoked Session after the tombstone expires. Deny states pass a
// zero deadline because their TTL starts when they are published.
func (s Store) writeUserSessionCache(entry *userSessionCacheEntry, cacheDeadline time.Time) error {
	if entry == nil || !common.RedisEnabled {
		return nil
	}
	now := time.Now()
	sessionExpiresAt := time.Unix(entry.ExpiresAt, 0)
	sessionTTL := sessionExpiresAt.Sub(now)
	var redisExpiration int64
	if entry.Status == identityrecord.UserSessionStatusActive {
		if cacheDeadline.IsZero() {
			return identitydomain.ErrSessionInvalid
		}
		cacheTTL := cacheDeadline.Sub(now)
		if cacheTTL <= 0 {
			return errUserSessionCacheObservationStale
		}
		if sessionTTL <= 0 {
			return identitydomain.ErrSessionInactive
		}
		cacheExpiresAt := cacheDeadline
		if sessionExpiresAt.Before(cacheExpiresAt) {
			cacheExpiresAt = sessionExpiresAt
		}
		if cacheExpiresAt.Sub(now) < time.Millisecond {
			return errUserSessionCacheObservationStale
		}
		redisExpiration = cacheExpiresAt.UnixMilli()
	} else {
		ttl := min(sessionTTL, time.Duration(s.env.UserCacheTTLSeconds())*time.Second)
		if ttl <= 0 {
			ttl = time.Second
		}
		redisExpiration = ttl.Milliseconds()
		if redisExpiration <= 0 {
			redisExpiration = 1
		}
	}
	entry.CacheSchema = userSessionCacheSchema
	const script = `
local current_status = redis.call('HGET', KEYS[1], 'Status')
local current_version = tonumber(redis.call('HGET', KEYS[1], 'Version') or '0')
if ARGV[5] == 'active' and (current_status == 'revoking' or current_status == 'revoked') then
  return 0
end
if current_version > tonumber(ARGV[3]) then
  return 0
end
redis.call('HSET', KEYS[1],
  'SID', ARGV[1], 'UserID', ARGV[2], 'Version', ARGV[3],
  'UserAuthVersion', ARGV[4], 'Status', ARGV[5],
  'LoginMethod', ARGV[6], 'IP', ARGV[7], 'UserAgent', ARGV[8],
  'CreatedAt', ARGV[9], 'LastActiveAt', ARGV[10], 'ExpiresAt', ARGV[11],
  'RevokedAt', ARGV[12], 'RevokedReason', ARGV[13], 'CacheSchema', ARGV[14])
if ARGV[5] == 'active' then
  redis.call('PEXPIREAT', KEYS[1], ARGV[15])
else
  redis.call('PEXPIRE', KEYS[1], ARGV[15])
end
return 1`
	result, err := common.RDB.Eval(context.Background(), script, []string{s.userSessionCacheKey(entry.SID)},
		entry.SID, entry.UserID, entry.Version, entry.UserAuthVersion, entry.Status,
		entry.LoginMethod, entry.IP, entry.UserAgent, entry.CreatedAt, entry.LastActiveAt,
		entry.ExpiresAt, entry.RevokedAt, entry.RevokedReason, entry.CacheSchema, redisExpiration,
	).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return identitydomain.ErrSessionInactive
	}
	if entry.Status == identityrecord.UserSessionStatusActive {
		completedAt := time.Now()
		if !completedAt.Before(cacheDeadline) {
			return errUserSessionCacheObservationStale
		}
		if !completedAt.Before(sessionExpiresAt) {
			return identitydomain.ErrSessionInactive
		}
	}
	return nil
}

func (s Store) confirmUserSessionActiveSnapshot(session *identityrecord.UserSession) error {
	if session == nil || session.SID == "" || session.UserID <= 0 || session.Version <= 0 || session.UserAuthVersion <= 0 {
		return identitydomain.ErrSessionInvalid
	}
	var count int64
	err := s.env.DB().Model(&identityrecord.UserSession{}).
		Where(
			"sid = ? AND user_id = ? AND status = ? AND revoked_at = ? AND expires_at > ? AND version = ? AND user_auth_version = ?",
			session.SID,
			session.UserID,
			identityrecord.UserSessionStatusActive,
			0,
			time.Now().Unix(),
			session.Version,
			session.UserAuthVersion,
		).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return identitydomain.ErrSessionInactive
	}
	return nil
}

func (s Store) writeUserSessionDenyFence(session *identityrecord.UserSession, status string, now int64, reason string) error {
	if !common.RedisEnabled {
		return nil
	}
	entry := s.cacheEntry(session)
	entry.Status = status
	entry.RevokedAt = now
	entry.RevokedReason = reason
	return s.writeUserSessionCache(entry, time.Time{})
}

func (s Store) ListActiveUserSessions(userID int, currentSID string, now int64) ([]identityrecord.UserSession, error) {
	if userID <= 0 {
		return nil, identitydomain.ErrSessionInvalid
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	var authVersion int64
	if err := s.env.DB().Model(&accountrecord.User{}).Where("id = ?", userID).Select("auth_version").Find(&authVersion).Error; err != nil {
		return nil, err
	}
	if authVersion <= 0 {
		return nil, identitydomain.ErrSessionInvalid
	}
	sessions := make([]identityrecord.UserSession, 0, userSessionListLimit)
	if currentSID != "" {
		var current []identityrecord.UserSession
		if err := s.env.DB().Where(
			"user_id = ? AND user_auth_version = ? AND status = ? AND expires_at > ? AND sid = ?",
			userID,
			authVersion,
			identityrecord.UserSessionStatusActive,
			now,
			currentSID,
		).Limit(1).Find(&current).Error; err != nil {
			return nil, err
		}
		if len(current) == 1 {
			sessions = append(sessions, current[0])
		}
	}
	remainingLimit := userSessionListLimit - len(sessions)

	otherQuery := s.env.DB().Where(
		"user_id = ? AND user_auth_version = ? AND status = ? AND expires_at > ?",
		userID,
		authVersion,
		identityrecord.UserSessionStatusActive,
		now,
	)
	if currentSID != "" {
		otherQuery = otherQuery.Where("sid <> ?", currentSID)
	}
	var others []identityrecord.UserSession
	if err := otherQuery.Order("last_active_at DESC").Order("created_at DESC").Limit(remainingLimit).Find(&others).Error; err != nil {
		return nil, err
	}
	sessions = append(sessions, others...)
	return sessions, nil
}

// RotateUserSessionRefresh atomically rotates HMAC digests. The UPDATE itself
// is a compare-and-swap so SQLite, where LockForUpdate is intentionally a
// no-op, has the same single-winner behavior as MySQL and PostgreSQL. Only a
// recognized previous digest outside its grace window is treated as reuse;
// an unknown secret never revokes the victim session.
func (s Store) RotateUserSessionRefresh(userID int, sid, presentedHash, nextHash string, now int64, grace time.Duration) (*identityrecord.UserSession, error) {
	if userID <= 0 || sid == "" || presentedHash == "" || nextHash == "" || hmac.Equal([]byte(presentedHash), []byte(nextHash)) {
		return nil, identitydomain.ErrSessionInvalid
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	graceSeconds := int64(grace / time.Second)
	if graceSeconds < 0 {
		return nil, identitydomain.ErrSessionInvalid
	}
	for range 3 {
		cacheDeadline := s.userSessionCacheDeadline()
		var session identityrecord.UserSession
		if err := s.env.DB().Where("sid = ? AND user_id = ?", sid, userID).First(&session).Error; err != nil {
			return nil, err
		}
		if session.Status != identityrecord.UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= now {
			return nil, identitydomain.ErrSessionInactive
		}

		if hmac.Equal([]byte(session.RefreshHash), []byte(presentedHash)) {
			result := s.env.DB().Model(&identityrecord.UserSession{}).
				Where("sid = ? AND user_id = ? AND status = ? AND revoked_at = ? AND expires_at > ? AND refresh_hash = ?",
					sid, userID, identityrecord.UserSessionStatusActive, 0, now, presentedHash).
				Updates(map[string]any{
					"previous_refresh_hash": session.RefreshHash,
					"previous_valid_until":  now + graceSeconds,
					"refresh_hash":          nextHash,
					"last_active_at":        now,
				})
			if result.Error != nil {
				return nil, result.Error
			}
			if result.RowsAffected == 0 {
				continue
			}
			session.PreviousRefreshHash = session.RefreshHash
			session.PreviousValidUntil = now + graceSeconds
			session.RefreshHash = nextHash
			session.LastActiveAt = now
			if err := s.writeUserSessionCache(s.cacheEntry(&session), cacheDeadline); err != nil {
				if errors.Is(err, errUserSessionCacheObservationStale) {
					if confirmErr := s.confirmUserSessionActiveSnapshot(&session); confirmErr != nil {
						return nil, confirmErr
					}
				} else if errors.Is(err, identitydomain.ErrSessionInactive) {
					return nil, err
				} else {
					common.SysLog("failed to update rotated user session cache: " + err.Error())
				}
			}
			return &session, nil
		}

		if session.PreviousRefreshHash == "" || !hmac.Equal([]byte(session.PreviousRefreshHash), []byte(presentedHash)) {
			return nil, identitydomain.ErrSessionRefreshInvalid
		}
		if now <= session.PreviousValidUntil {
			return &session, identitydomain.ErrSessionRefreshRace
		}

		// Once a known previous token is replayed outside the grace window the
		// whole token family is compromised. Publish the deny fence first, then
		// revoke the active row regardless of a concurrent refresh rotation.
		if err := s.writeUserSessionDenyFence(&session, identityrecord.UserSessionStatusRevoking, now, "refresh_reuse"); err != nil {
			return nil, err
		}
		result := s.env.DB().Model(&identityrecord.UserSession{}).
			Where("sid = ? AND user_id = ? AND status = ? AND revoked_at = ? AND expires_at > ?",
				sid, userID, identityrecord.UserSessionStatusActive, 0, now).
			Updates(map[string]any{
				"status":         identityrecord.UserSessionStatusRevoked,
				"revoked_at":     now,
				"revoked_reason": "refresh_reuse",
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, identitydomain.ErrSessionInactive
		}
		session.Status = identityrecord.UserSessionStatusRevoked
		session.RevokedAt = now
		session.RevokedReason = "refresh_reuse"
		if err := s.writeUserSessionCache(s.cacheEntry(&session), time.Time{}); err != nil {
			common.SysLog("failed to cache refresh-reuse session revoke: " + err.Error())
		}
		return nil, identitydomain.ErrSessionRefreshReuse
	}
	return nil, identitydomain.ErrSessionRefreshInvalid
}

func (s Store) RevokeUserSession(userID int, sid, reason string) (bool, error) {
	if userID <= 0 || sid == "" {
		return false, identitydomain.ErrSessionInvalid
	}
	now := time.Now().Unix()
	var candidate identityrecord.UserSession
	if err := s.env.DB().Where("sid = ? AND user_id = ?", sid, userID).First(&candidate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if candidate.Status != identityrecord.UserSessionStatusActive || candidate.RevokedAt != 0 || candidate.ExpiresAt <= now {
		return false, nil
	}
	if err := s.writeUserSessionDenyFence(&candidate, identityrecord.UserSessionStatusRevoking, now, reason); err != nil {
		return false, err
	}

	var revoked bool
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		var current identityrecord.UserSession
		if err := locking.LockForUpdate(tx).Where("sid = ? AND user_id = ?", sid, userID).First(&current).Error; err != nil {
			return err
		}
		if current.Status != identityrecord.UserSessionStatusActive || current.RevokedAt != 0 || current.ExpiresAt <= now {
			return nil
		}
		result := tx.Model(&identityrecord.UserSession{}).Where("sid = ? AND status = ?", sid, identityrecord.UserSessionStatusActive).Updates(map[string]any{
			"status":         identityrecord.UserSessionStatusRevoked,
			"revoked_at":     now,
			"revoked_reason": reason,
		})
		if result.Error != nil {
			return result.Error
		}
		revoked = result.RowsAffected == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	if revoked {
		candidate.Status = identityrecord.UserSessionStatusRevoked
		candidate.RevokedAt = now
		candidate.RevokedReason = reason
		if err := s.writeUserSessionCache(s.cacheEntry(&candidate), time.Time{}); err != nil {
			common.SysLog("failed to finalize user session revoke tombstone: " + err.Error())
		}
	}
	return revoked, nil
}

// RevokeUserSessionByRefreshHash is used when logout is authenticated only by
// the HttpOnly refresh cookie. Possession of a SID alone is insufficient. The
// immediately previous digest is accepted only inside the refresh race window.
func (s Store) RevokeUserSessionByRefreshHash(sid, presentedHash, reason string) (bool, error) {
	if sid == "" || presentedHash == "" {
		return false, identitydomain.ErrSessionInvalid
	}
	now := time.Now().Unix()
	var session identityrecord.UserSession
	var revoked bool
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if err := locking.LockForUpdate(tx).Where("sid = ?", sid).First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if session.Status != identityrecord.UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= now {
			return nil
		}
		validCurrent := hmac.Equal([]byte(session.RefreshHash), []byte(presentedHash))
		validPrevious := session.PreviousRefreshHash != "" && now <= session.PreviousValidUntil &&
			hmac.Equal([]byte(session.PreviousRefreshHash), []byte(presentedHash))
		if !validCurrent && !validPrevious {
			return nil
		}
		if err := s.writeUserSessionDenyFence(&session, identityrecord.UserSessionStatusRevoking, now, reason); err != nil {
			return err
		}
		result := tx.Model(&identityrecord.UserSession{}).Where("sid = ? AND status = ?", sid, identityrecord.UserSessionStatusActive).Updates(map[string]any{
			"status":         identityrecord.UserSessionStatusRevoked,
			"revoked_at":     now,
			"revoked_reason": reason,
		})
		if result.Error != nil {
			return result.Error
		}
		revoked = result.RowsAffected == 1
		if revoked {
			session.Status = identityrecord.UserSessionStatusRevoked
			session.RevokedAt = now
			session.RevokedReason = reason
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if revoked {
		if err := s.writeUserSessionCache(s.cacheEntry(&session), time.Time{}); err != nil {
			common.SysLog("failed to finalize refresh-authenticated session revoke tombstone: " + err.Error())
		}
	}
	return revoked, nil
}

// AdvanceUserSessionAuthVersion preserves one browser session across a
// user-level security-version change. Both old access JWTs and concurrent
// updates are invalidated by advancing the per-session version as well.
func (s Store) AdvanceUserSessionAuthVersion(userID int, sid string, expectedSessionVersion, expectedUserAuthVersion, nextUserAuthVersion int64) (*identityrecord.UserSession, error) {
	if userID <= 0 || sid == "" || expectedSessionVersion <= 0 || expectedUserAuthVersion <= 0 || nextUserAuthVersion <= expectedUserAuthVersion {
		return nil, identitydomain.ErrSessionInvalid
	}
	cacheDeadline := s.userSessionCacheDeadline()
	now := time.Now().Unix()
	var session identityrecord.UserSession
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if err := locking.LockForUpdate(tx).Where("sid = ? AND user_id = ?", sid, userID).First(&session).Error; err != nil {
			return err
		}
		if session.Status != identityrecord.UserSessionStatusActive || session.ExpiresAt <= now ||
			session.Version != expectedSessionVersion || session.UserAuthVersion != expectedUserAuthVersion {
			return identitydomain.ErrSessionInactive
		}
		session.Version++
		session.UserAuthVersion = nextUserAuthVersion
		session.LastActiveAt = now
		result := tx.Model(&identityrecord.UserSession{}).
			Where("sid = ? AND status = ? AND version = ? AND user_auth_version = ?", sid, identityrecord.UserSessionStatusActive, expectedSessionVersion, expectedUserAuthVersion).
			Updates(map[string]any{
				"version":           session.Version,
				"user_auth_version": session.UserAuthVersion,
				"last_active_at":    session.LastActiveAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return identitydomain.ErrSessionInactive
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.writeUserSessionCache(s.cacheEntry(&session), cacheDeadline); err != nil {
		if errors.Is(err, errUserSessionCacheObservationStale) {
			if confirmErr := s.confirmUserSessionActiveSnapshot(&session); confirmErr != nil {
				return nil, confirmErr
			}
		} else {
			return nil, err
		}
	}
	return &session, nil
}

func (s Store) RevokeOtherUserSessions(userID int, currentSID, reason string) (int64, error) {
	return s.revokeUserSessions(userID, currentSID, reason)
}

func (s Store) RevokeAllUserSessions(userID int, reason string) (int64, error) {
	return s.revokeUserSessions(userID, "", reason)
}

func (s Store) revokeUserSessions(userID int, excludedSID, reason string) (int64, error) {
	if userID <= 0 {
		return 0, identitydomain.ErrSessionInvalid
	}
	now := time.Now().Unix()
	var totalAffected int64
	for {
		query := s.env.DB().Where("user_id = ? AND status = ? AND expires_at > ?", userID, identityrecord.UserSessionStatusActive, now)
		if excludedSID != "" {
			query = query.Where("sid <> ?", excludedSID)
		}
		var candidates []identityrecord.UserSession
		if err := query.Order("sid").Limit(userSessionRevokeBatchSize).Find(&candidates).Error; err != nil {
			return totalAffected, err
		}
		if len(candidates) == 0 {
			return totalAffected, nil
		}
		for i := range candidates {
			if err := s.writeUserSessionDenyFence(&candidates[i], identityrecord.UserSessionStatusRevoking, now, reason); err != nil {
				return totalAffected, err
			}
		}

		sids := make([]string, 0, len(candidates))
		for i := range candidates {
			sids = append(sids, candidates[i].SID)
		}
		var affected int64
		var revoked []identityrecord.UserSession
		err := s.env.DB().Transaction(func(tx *gorm.DB) error {
			if err := locking.LockForUpdate(tx).Where("sid IN ? AND status = ?", sids, identityrecord.UserSessionStatusActive).Find(&revoked).Error; err != nil {
				return err
			}
			if len(revoked) == 0 {
				return nil
			}
			lockedSIDs := make([]string, 0, len(revoked))
			for i := range revoked {
				lockedSIDs = append(lockedSIDs, revoked[i].SID)
			}
			result := tx.Model(&identityrecord.UserSession{}).Where("sid IN ? AND status = ?", lockedSIDs, identityrecord.UserSessionStatusActive).Updates(map[string]any{
				"status":         identityrecord.UserSessionStatusRevoked,
				"revoked_at":     now,
				"revoked_reason": reason,
			})
			affected = result.RowsAffected
			return result.Error
		})
		if err != nil {
			return totalAffected, err
		}
		totalAffected += affected
		for i := range revoked {
			revoked[i].Status = identityrecord.UserSessionStatusRevoked
			revoked[i].RevokedAt = now
			revoked[i].RevokedReason = reason
			if err := s.writeUserSessionCache(s.cacheEntry(&revoked[i]), time.Time{}); err != nil {
				common.SysLog("failed to finalize bulk user session revoke tombstone: " + err.Error())
			}
		}
	}
}

func (s Store) DeleteExpiredUserSessions(now int64) error {
	if now <= 0 {
		now = time.Now().Unix()
	}
	if common.UserSessionRevokedRetentionDays <= 0 || common.UserSessionIssuanceWindowSeconds <= 0 {
		return identitydomain.ErrSessionInvalid
	}
	issuanceCutoff := now - common.UserSessionIssuanceWindowSeconds
	revokedBefore := now - int64(common.UserSessionRevokedRetentionDays)*24*60*60
	return s.deleteExpiredUserSessionsBefore(now, issuanceCutoff, revokedBefore)
}

func (s Store) DeleteOldRevokedUserSessions(now int64) error {
	if now <= 0 {
		now = time.Now().Unix()
	}
	if common.UserSessionRevokedRetentionDays <= 0 || common.UserSessionIssuanceWindowSeconds <= 0 {
		return identitydomain.ErrSessionInvalid
	}
	issuanceCutoff := now - common.UserSessionIssuanceWindowSeconds
	revokedBefore := now - int64(common.UserSessionRevokedRetentionDays)*24*60*60
	return s.deleteRevokedUserSessionsBefore(revokedBefore, issuanceCutoff)
}

func (s Store) deleteExpiredUserSessionsBefore(expiredBefore, issuanceCutoff, revokedBefore int64) error {
	for {
		var sids []string
		if err := s.env.DB().Model(&identityrecord.UserSession{}).
			Where(
				"expires_at < ? AND created_at <= ? AND (status <> ? OR revoked_at <= 0 OR revoked_at < ?)",
				expiredBefore,
				issuanceCutoff,
				identityrecord.UserSessionStatusRevoked,
				revokedBefore,
			).
			Order("expires_at").Limit(userSessionCleanupScanLimit).Pluck("sid", &sids).Error; err != nil {
			return err
		}
		if len(sids) == 0 {
			return nil
		}
		for start := 0; start < len(sids); start += userSessionCleanupBatchSize {
			end := min(start+userSessionCleanupBatchSize, len(sids))
			if err := s.env.DB().Where("sid IN ?", sids[start:end]).
				Where(
					"expires_at < ? AND created_at <= ? AND (status <> ? OR revoked_at <= 0 OR revoked_at < ?)",
					expiredBefore,
					issuanceCutoff,
					identityrecord.UserSessionStatusRevoked,
					revokedBefore,
				).
				Delete(&identityrecord.UserSession{}).Error; err != nil {
				return err
			}
		}
	}
}

func (s Store) deleteRevokedUserSessionsBefore(revokedBefore, issuanceCutoff int64) error {
	for {
		var sids []string
		if err := s.env.DB().Model(&identityrecord.UserSession{}).
			Where(
				"status = ? AND revoked_at > 0 AND revoked_at < ? AND created_at <= ?",
				identityrecord.UserSessionStatusRevoked,
				revokedBefore,
				issuanceCutoff,
			).
			Order("revoked_at").Limit(userSessionCleanupScanLimit).Pluck("sid", &sids).Error; err != nil {
			return err
		}
		if len(sids) == 0 {
			return nil
		}
		for start := 0; start < len(sids); start += userSessionCleanupBatchSize {
			end := min(start+userSessionCleanupBatchSize, len(sids))
			if err := s.env.DB().Where("sid IN ?", sids[start:end]).
				Where(
					"status = ? AND revoked_at > 0 AND revoked_at < ? AND created_at <= ?",
					identityrecord.UserSessionStatusRevoked,
					revokedBefore,
					issuanceCutoff,
				).
				Delete(&identityrecord.UserSession{}).Error; err != nil {
				return err
			}
		}
	}
}
