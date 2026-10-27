package session

import (
	"time"

	"github.com/QuantumNous/new-api/model"
)

type ArtifactCleanup struct{}

func (ArtifactCleanup) CountIssued(since int64) (int64, error) {
	return model.CountUserSessionsCreatedSince(0, since)
}
func (ArtifactCleanup) DeleteExpiredSessions(now int64) error {
	return model.DeleteExpiredUserSessions(now)
}
func (ArtifactCleanup) DeleteRevokedSessions(now int64) error {
	return model.DeleteOldRevokedUserSessions(now)
}
func (ArtifactCleanup) DeleteExpiredFlows(now time.Time) error {
	return model.DeleteExpiredAuthFlows(now)
}
