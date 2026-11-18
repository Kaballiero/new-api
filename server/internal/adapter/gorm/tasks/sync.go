package tasks

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

type SyncStore struct{ BillingStore }

func (s SyncStore) Insert(ctx context.Context, task *domain.Task) error {
	row := record.FromDomain(task)
	err := s.Store.InsertTask(ctx, row)
	record.RestoreDomain(task, row)
	return err
}
func (s SyncStore) Unique(taskID string) (*domain.Task, bool, error) {
	row, found, err := s.Store.GetUniqueByOnlyTaskId(taskID)
	return record.ToDomain(row), found, err
}

func (s SyncStore) UnknownSubmissionHolds(submittedBefore, afterID int64, limit int) ([]*domain.Task, error) {
	rows, err := s.Store.GetUnknownSubmissionHolds(submittedBefore, afterID, limit)
	tasks := make([]*domain.Task, len(rows))
	for i := range rows {
		tasks[i] = record.ToDomain(rows[i])
	}
	return tasks, err
}

func (s SyncStore) ParkUnknownSubmissionHold(task *domain.Task) error {
	return s.Store.ParkUnknownSubmissionHold(task.ID)
}

func (s SyncStore) ManualHolds(afterID int64, limit int) ([]*domain.Task, error) {
	rows, err := s.Store.GetManualHolds(afterID, limit)
	tasks := make([]*domain.Task, len(rows))
	for i := range rows {
		tasks[i] = record.ToDomain(rows[i])
	}
	return tasks, err
}

func (s SyncStore) MarkAlerted(task *domain.Task, alertedAt int64) error {
	return s.Store.MarkManualHoldAlerted(task.ID, alertedAt)
}

func (s SyncStore) PendingEstimateSettlements(afterID int64, limit int) ([]*domain.Task, error) {
	rows, err := s.Store.GetPendingEstimateSettlements(afterID, limit)
	tasks := make([]*domain.Task, len(rows))
	for i := range rows {
		tasks[i] = record.ToDomain(rows[i])
	}
	return tasks, err
}

func (s SyncStore) EstimateCharges(afterFinishTime, afterID int64, limit int) ([]*domain.Task, error) {
	rows, err := s.Store.GetEstimateCharges(afterFinishTime, afterID, limit)
	tasks := make([]*domain.Task, len(rows))
	for i := range rows {
		tasks[i] = record.ToDomain(rows[i])
	}
	return tasks, err
}

func (s SyncStore) MarkEstimateAlerted(task *domain.Task, alertedAt int64) error {
	return s.Store.MarkEstimateAlerted(task.ID, alertedAt)
}

type SyncProjection struct{}

func (SyncProjection) Project(task *domain.Task) {
	task.Data, _ = common.Marshal(domain.OpenRouterSyncData(task))
}
