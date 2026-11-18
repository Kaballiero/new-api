package refuse_sync_reservation

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refusalFixture struct{ revisions int }

func (f *refusalFixture) UpdateRevision(*domain.Task, int) error { f.revisions++; return nil }
func (f *refusalFixture) Project(*domain.Task)                   {}

func TestRefuseClosesOnlyAnIntentWithoutHold(t *testing.T) {
	now := time.Date(2026, time.November, 18, 1, 37, 51, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		pending       bool
		reservedQuota int
		leaseUntil    int64
		refused       bool
	}{
		{name: "refused reservation closes", pending: true, leaseUntil: now.Add(time.Minute).Unix(), refused: true},
		{name: "committed reservation is kept", pending: true, reservedQuota: 50, leaseUntil: now.Add(time.Minute).Unix()},
		{name: "submitted task is kept", leaseUntil: now.Add(time.Minute).Unix()},
		{name: "expired lease is left to the poller", pending: true, leaseUntil: now.Unix()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &domain.Task{Platform: string(domain.TaskPlatformOpenRouterAudio), Status: domain.TaskStatusNotStart,
				PrivateData: domain.TaskPrivateData{OpenRouterAudio: &domain.OpenRouterSyncTask{LeaseUntil: tc.leaseUntil, ResultReasonTracked: true},
					Reconciliation: &domain.TaskReconciliation{ReservationPending: tc.pending, Required: true, ReservedQuota: tc.reservedQuota}}}
			fixture := &refusalFixture{}
			err := NewUsecase(fixture, fixture, func() time.Time { return now }).Execute(t.Context(), task)
			if !tc.refused {
				require.Error(t, err)
				assert.Zero(t, fixture.revisions)
				assert.Equal(t, domain.TaskStatus(domain.TaskStatusNotStart), task.Status)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, fixture.revisions)
			assert.Equal(t, domain.TaskStatus(domain.TaskStatusFailure), task.Status)
			assert.False(t, task.PrivateData.Reconciliation.ReservationPending)
			assert.False(t, task.PrivateData.Reconciliation.Required)
			assert.True(t, task.PrivateData.Reconciliation.Finalized)
			assert.Equal(t, "settled", domain.OpenRouterSyncReconciliation(task)["billing_state"])
			assert.Equal(t, domain.SyncReasonReservationRefused, domain.OpenRouterSyncReconciliation(task)["reason_code"])
		})
	}
}
