package alert_estimate_settlements

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
	tasks   []*domain.Task
	markErr error
	// failMarkAfter fails every mark after this many succeeded.
	failMarkAfter int
	marked        map[int64]int64
}

func (s *fakeStore) EstimateCharges(afterFinishTime, afterID int64, limit int) ([]*domain.Task, error) {
	var page []*domain.Task
	for _, task := range s.tasks {
		if (task.FinishTime > afterFinishTime || task.FinishTime == afterFinishTime && task.ID > afterID) && len(page) < limit {
			page = append(page, task)
		}
	}
	return page, nil
}

func (s *fakeStore) MarkEstimateAlerted(task *domain.Task, alertedAt int64) error {
	if s.markErr != nil && len(s.marked) >= s.failMarkAfter {
		return s.markErr
	}
	s.marked[task.ID] = alertedAt
	task.PrivateData.Reconciliation.EstimateAlertedAt = alertedAt
	return nil
}

type fakeAlerts struct {
	summaries [][]string
	err       error
}

func (a *fakeAlerts) EstimateSettlements(tasks []*domain.Task) error {
	var taskIDs []string
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.TaskID)
	}
	a.summaries = append(a.summaries, taskIDs)
	return a.err
}

type fakeDiagnostics struct{ warnings []string }

func (d *fakeDiagnostics) Warn(_ context.Context, message string) {
	d.warnings = append(d.warnings, message)
}

// estimateCharge is a task finished at finishTime whose estimate charge moved
// balances; a video without an estimate is selected but never reported.
func estimateCharge(id, finishTime int64, estimated bool) *domain.Task {
	return &domain.Task{ID: id, TaskID: fmt.Sprintf("task_%d", id), FinishTime: finishTime,
		PrivateData: domain.TaskPrivateData{Reconciliation: &domain.TaskReconciliation{UpstreamCostEstimated: estimated, MainApplied: true}}}
}

func TestExecuteReportsEstimateChargesOnceInOneSummary(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	windowStart := now.Add(-estimateAlertWindow).Unix()
	var tasks []*domain.Task
	for id := range int64(pageSize + 1) {
		tasks = append(tasks, estimateCharge(id+1, windowStart+id, true))
	}
	tasks = append(tasks, estimateCharge(pageSize+2, windowStart+1, false), estimateCharge(pageSize+3, windowStart-1, true))
	store := &fakeStore{tasks: tasks, marked: map[int64]int64{}}
	alerts, diagnostics := &fakeAlerts{}, &fakeDiagnostics{}
	usecase := NewUsecase(store, alerts, diagnostics, func() time.Time { return now })

	require.NoError(t, usecase.Execute(context.Background()))
	require.Len(t, alerts.summaries, 1)
	assert.Len(t, alerts.summaries[0], pageSize+1, "only estimate charges of the window")
	assert.Len(t, store.marked, pageSize+1)
	assert.Empty(t, diagnostics.warnings, "a charge older than the first window is not reported as aged out")

	require.NoError(t, usecase.Execute(context.Background()))
	assert.Len(t, alerts.summaries, 1)
}

func TestExecuteRetriesASummaryUntilItAgesOut(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	charge := estimateCharge(1, now.Unix(), true)
	store := &fakeStore{tasks: []*domain.Task{charge}, marked: map[int64]int64{}}
	alerts, diagnostics := &fakeAlerts{err: errors.New("telegram gateway unavailable")}, &fakeDiagnostics{}
	usecase := NewUsecase(store, alerts, diagnostics, func() time.Time { return now })

	require.ErrorIs(t, usecase.Execute(context.Background()), alerts.err)
	assert.Empty(t, store.marked)

	now = now.Add(estimateAlertWindow + time.Second)
	require.NoError(t, usecase.Execute(context.Background()))
	require.Len(t, diagnostics.warnings, 1)
	assert.Contains(t, diagnostics.warnings[0], "left the alert window unreported: task_1")

	require.NoError(t, usecase.Execute(context.Background()))
	assert.Len(t, diagnostics.warnings, 1, "an aged-out charge is logged once")
	assert.Len(t, alerts.summaries, 1, "an aged-out charge is no longer summarized")
}

func TestExecuteResendsOnlyTheChargesItCouldNotMark(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{tasks: []*domain.Task{estimateCharge(1, now.Unix(), true), estimateCharge(2, now.Unix(), true)},
		markErr: errors.New("database unavailable"), failMarkAfter: 1, marked: map[int64]int64{}}
	alerts := &fakeAlerts{}
	usecase := NewUsecase(store, alerts, &fakeDiagnostics{}, func() time.Time { return now })

	require.ErrorIs(t, usecase.Execute(context.Background()), store.markErr)
	assert.Len(t, store.marked, 1)

	store.markErr = nil
	require.NoError(t, usecase.Execute(context.Background()))
	require.Len(t, alerts.summaries, 2)
	assert.Equal(t, []string{"task_2"}, alerts.summaries[1])
}
