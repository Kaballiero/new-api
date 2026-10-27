package identity

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"time"
)

type TestUserSessionCacheEntry = userSessionCacheEntry

const TestUserSessionCacheSchema = userSessionCacheSchema
const TestUserSessionListLimit = userSessionListLimit
const TestUserSessionRevokeBatchSize = userSessionRevokeBatchSize
const TestUserSessionCleanupScanLimit = userSessionCleanupScanLimit

var TestErrUserSessionCacheObservationStale = errUserSessionCacheObservationStale

func (s Store) TestCacheEntry(session *record.UserSession) *userSessionCacheEntry {
	return s.cacheEntry(session)
}
func SessionForTest(entry *userSessionCacheEntry) *record.UserSession { return entry.session() }
func (s Store) TestUserSessionCacheKey(sid string) string             { return s.userSessionCacheKey(sid) }
func (s Store) TestUserSessionCacheDeadline() time.Time               { return s.userSessionCacheDeadline() }
func (s Store) TestGetUserSessionCache(sid string) (*userSessionCacheEntry, error) {
	return s.getUserSessionCache(sid)
}
func (s Store) TestWriteUserSessionCache(entry *userSessionCacheEntry, deadline time.Time) error {
	return s.writeUserSessionCache(entry, deadline)
}
