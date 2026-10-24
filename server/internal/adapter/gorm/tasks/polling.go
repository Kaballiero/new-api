package tasks

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// PollingStore maps technical records without changing inventory or claim SQL.
type PollingStore struct{ Store Store }

func (s PollingStore) TimedOut(cutoffUnix int64, limit int) []*domain.Task {
	rows := s.Store.GetTimedOutUnfinishedTasks(cutoffUnix, limit)
	if rows == nil {
		return nil
	}
	tasks := make([]*domain.Task, len(rows))
	for i := range rows {
		tasks[i] = record.ToDomain(rows[i])
	}
	return tasks
}

func (s PollingStore) UnfinishedPage(limit int, afterID int64) []*domain.Task {
	rows := s.Store.GetUnfinishedSyncTaskPage(limit, afterID)
	if rows == nil {
		return nil
	}
	tasks := make([]*domain.Task, len(rows))
	for i := range rows {
		tasks[i] = record.ToDomain(rows[i])
	}
	return tasks
}

func (s PollingStore) BulkUpdate(ids []int64, fields map[string]any) error {
	return s.Store.TaskBulkUpdateByID(ids, fields)
}

func (s PollingStore) Transition(task *domain.Task, fromStatus domain.TaskStatus) (bool, error) {
	row := record.FromDomain(task)
	won, err := s.Store.UpdateTaskWithStatus(row, record.TaskStatus(fromStatus))
	record.RestoreDomain(task, row)
	return won, err
}

// MidjourneyPollingStore retains the separate legacy Midjourney record contract.
type MidjourneyPollingStore struct{ Store Store }

func (s MidjourneyPollingStore) Unfinished() []*domain.Midjourney {
	rows := s.Store.GetAllUnFinishTasks()
	if rows == nil {
		return nil
	}
	tasks := make([]*domain.Midjourney, len(rows))
	for i := range rows {
		tasks[i] = MidjourneyToDomain(rows[i])
	}
	return tasks
}

func (s MidjourneyPollingStore) BulkUpdateByIDs(ids []int, fields map[string]any) error {
	return s.Store.MjBulkUpdateByTaskIds(ids, fields)
}

func (s MidjourneyPollingStore) BulkUpdateByTaskIDs(taskIDs []string, fields map[string]any) error {
	return s.Store.MjBulkUpdate(taskIDs, fields)
}

func (s MidjourneyPollingStore) Transition(task *domain.Midjourney, fromStatus string) (bool, error) {
	return s.Store.UpdateMidjourneyWithStatus(MidjourneyFromDomain(task), fromStatus)
}
