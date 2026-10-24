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

type SyncProjection struct{}

func (SyncProjection) Project(task *domain.Task) {
	task.Data, _ = common.Marshal(domain.OpenRouterSyncData(task))
}
