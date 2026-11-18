package alert_sync_reconciliation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	holds, unfinished []*domain.Task
	alerted           map[int64]int64
}

func (s *fakeStore) ManualHolds(afterID int64, limit int) ([]*domain.Task, error) {
	return taskPage(s.holds, afterID, limit), nil
}

func (s *fakeStore) Unfinished(afterID int64, limit int) ([]*domain.Task, error) {
	return taskPage(s.unfinished, afterID, limit), nil
}

func taskPage(tasks []*domain.Task, afterID int64, limit int) []*domain.Task {
	var page []*domain.Task
	for _, task := range tasks {
		if task.ID > afterID && len(page) < limit {
			page = append(page, task)
		}
	}
	return page
}

func (s *fakeStore) MarkAlerted(task *domain.Task, alertedAt int64) error {
	s.alerted[task.ID] = alertedAt
	task.PrivateData.OpenRouterSyncState().AlertedAt = alertedAt
	return nil
}

type fakeAlerts struct {
	summaries [][]string
	err       error
}

func (a *fakeAlerts) ManualHolds(holds []*domain.Task) error {
	var taskIDs []string
	for _, task := range holds {
		taskIDs = append(taskIDs, task.TaskID)
	}
	a.summaries = append(a.summaries, taskIDs)
	return a.err
}

func manualHold(id, alertedAt int64) *domain.Task {
	return &domain.Task{ID: id, TaskID: fmt.Sprintf("task_%d", id), Status: domain.TaskStatusFailure, PrivateData: domain.TaskPrivateData{
		OpenRouterSync: &domain.OpenRouterSyncTask{AlertedAt: alertedAt}, Reconciliation: &domain.TaskReconciliation{ReservedQuota: 50},
	}}
}

func TestExecuteReportsNewManualHoldsOnceInOneSummary(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var holds []*domain.Task
	for id := range int64(holdPageSize + 1) {
		holds = append(holds, manualHold(id+1, 0))
	}
	holds[0].PrivateData.OpenRouterSync.AlertedAt = 42
	store := &fakeStore{holds: holds, alerted: map[int64]int64{}}
	alerts := &fakeAlerts{}
	usecase := NewUsecase(store, alerts, func() time.Time { return now })

	require.NoError(t, usecase.Execute(context.Background()))
	require.Len(t, alerts.summaries, 1)
	assert.Len(t, alerts.summaries[0], holdPageSize)
	assert.Len(t, store.alerted, holdPageSize)
	assert.NotContains(t, store.alerted, holds[0].ID)
	assert.Equal(t, now.Unix(), store.alerted[holds[holdPageSize].ID])

	require.NoError(t, usecase.Execute(context.Background()))
	assert.Len(t, alerts.summaries, 1)
}

func TestExecuteLeavesHoldsUnmarkedWhenSummaryIsNotDelivered(t *testing.T) {
	store := &fakeStore{holds: []*domain.Task{manualHold(1, 0)}, alerted: map[int64]int64{}}
	alerts := &fakeAlerts{err: errors.New("telegram gateway unavailable")}
	usecase := NewUsecase(store, alerts, time.Now)

	require.ErrorIs(t, usecase.Execute(context.Background()), alerts.err)
	assert.Empty(t, store.alerted)

	alerts.err = nil
	require.NoError(t, usecase.Execute(context.Background()))
	assert.Len(t, alerts.summaries, 2)
	assert.Contains(t, store.alerted, int64(1))
}

func TestExecuteReportsSettlementsPendingLongerThanStallAge(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	pending := func(id int64, since time.Time) *domain.Task {
		return &domain.Task{ID: id, TaskID: fmt.Sprintf("task_%d", id), Status: domain.TaskStatusInProgress, PrivateData: domain.TaskPrivateData{
			OpenRouterSync: &domain.OpenRouterSyncTask{SettlementPendingSince: since.Unix()}, Reconciliation: &domain.TaskReconciliation{ReservedQuota: 50, SettlementPending: true},
		}}
	}
	recent := pending(2, now.Add(-29*time.Minute))
	awaitingCost := pending(3, now.Add(-time.Hour))
	awaitingCost.PrivateData.Reconciliation.SettlementPending = false
	videoTask := &domain.Task{ID: 4, TaskID: "task_4", Status: domain.TaskStatusInProgress}
	store := &fakeStore{
		unfinished: []*domain.Task{pending(1, now.Add(-30*time.Minute)), recent, awaitingCost, videoTask, pending(5, time.Unix(0, 0))},
		alerted:    map[int64]int64{},
	}
	alerts := &fakeAlerts{}
	usecase := NewUsecase(store, alerts, func() time.Time { return now })

	require.NoError(t, usecase.Execute(context.Background()))
	assert.Equal(t, [][]string{{"task_1", "task_5"}}, alerts.summaries, "a settlement pending before its start was recorded is reported")
	assert.Equal(t, map[int64]int64{1: now.Unix(), 5: now.Unix()}, store.alerted)
}

func TestExecuteSkipsHoldsThatMayKeepNoQuota(t *testing.T) {
	empty := manualHold(1, 0)
	empty.PrivateData.Reconciliation.ReservedQuota = 0
	planned := manualHold(2, 0)
	planned.PrivateData.Reconciliation.ReservedQuota, planned.PrivateData.Reconciliation.ReservationPending = 0, true
	planned.PrivateData.OpenRouterSync.PlannedQuota = 40
	uncommittedEmpty := manualHold(3, 0)
	uncommittedEmpty.PrivateData.Reconciliation.ReservedQuota, uncommittedEmpty.PrivateData.Reconciliation.ReservationPending = 0, true
	store := &fakeStore{holds: []*domain.Task{empty, planned, uncommittedEmpty}, alerted: map[int64]int64{}}
	alerts := &fakeAlerts{}
	usecase := NewUsecase(store, alerts, time.Now)

	require.NoError(t, usecase.Execute(context.Background()))
	assert.Equal(t, [][]string{{"task_2"}}, alerts.summaries, "a planned reserve may be held by the billing session")
}
