package session

import (
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"time"
)

type ArtifactCleanup struct{ SQL identitystore.Store }

func (op ArtifactCleanup) CountIssued(since int64) (int64, error) {
	return op.SQL.CountUserSessionsCreatedSince(0, since)
}
func (op ArtifactCleanup) DeleteExpiredSessions(now int64) error {
	return op.SQL.DeleteExpiredUserSessions(now)
}
func (op ArtifactCleanup) DeleteRevokedSessions(now int64) error {
	return op.SQL.DeleteOldRevokedUserSessions(now)
}
func (op ArtifactCleanup) DeleteExpiredFlows(now time.Time) error {
	return op.SQL.DeleteExpiredAuthFlows(now)
}
