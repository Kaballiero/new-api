package tasks

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// Reader projects rows without changing the query order or suppressed errors.
type Reader struct{ Store Store }

func (r Reader) GetOwn(userID int, taskID string) (*domain.Task, bool, error) {
	task, exists, err := r.Store.GetByTaskId(userID, taskID)
	return record.ToDomain(task), exists, err
}
func (r Reader) GetFirst(taskID string) (*domain.Task, bool, error) {
	task, exists, err := r.Store.GetByOnlyTaskId(taskID)
	return record.ToDomain(task), exists, err
}
func (r Reader) GetUnique(taskID string) (*domain.Task, bool, error) {
	task, exists, err := r.Store.GetUniqueByOnlyTaskId(taskID)
	return record.ToDomain(task), exists, err
}
func (r Reader) OwnerEnabled(userID int) (bool, error) {
	owner, err := r.Store.accounts.GetUserCache(userID)
	return owner != nil && owner.Status == common.UserStatusEnabled, err
}
func (r Reader) ListOwn(userID, offset, limit int, query domain.TaskQuery) []*domain.Task {
	rows := r.Store.TaskGetAllUserTask(userID, offset, limit, taskReadQuery(query))
	items := make([]*domain.Task, len(rows))
	for i, row := range rows {
		items[i] = record.ToDomain(row)
	}
	return items
}
func (r Reader) ListAll(offset, limit int, query domain.TaskQuery) []*domain.Task {
	rows := r.Store.TaskGetAllTasks(offset, limit, taskReadQuery(query))
	items := make([]*domain.Task, len(rows))
	for i, row := range rows {
		items[i] = record.ToDomain(row)
	}
	return items
}
func (r Reader) CountOwn(userID int, query domain.TaskQuery) int64 {
	return r.Store.TaskCountAllUserTask(userID, taskReadQuery(query))
}
func (r Reader) CountAll(query domain.TaskQuery) int64 {
	return r.Store.TaskCountAllTasks(taskReadQuery(query))
}
func (r Reader) FillUsernames(tasks []*domain.Task) {
	userIDs := make(map[int]struct{})
	for _, task := range tasks {
		userIDs[task.UserId] = struct{}{}
	}
	users := make(map[int]*accountcache.UserBase, len(userIDs))
	for userID := range userIDs {
		if user, err := r.Store.accounts.GetUserCache(userID); err == nil {
			users[userID] = user
		}
	}
	for _, task := range tasks {
		if user, ok := users[task.UserId]; ok {
			task.Username = user.Username
		}
	}
}
func taskReadQuery(query domain.TaskQuery) record.SyncTaskQueryParams {
	return record.SyncTaskQueryParams{
		Platform:       constant.TaskPlatform(query.Platform),
		TaskID:         query.TaskID,
		Status:         query.Status,
		Action:         query.Action,
		StartTimestamp: query.StartTimestamp,
		EndTimestamp:   query.EndTimestamp,
		ChannelID:      query.ChannelID,
	}
}

func (r Reader) ListOwnMidjourney(userID, offset, limit int, query domain.MidjourneyQuery) []*domain.Midjourney {
	rows := r.Store.GetAllUserTask(userID, offset, limit, midjourneyReadQuery(query))
	if rows == nil {
		return nil
	}
	items := make([]*domain.Midjourney, len(rows))
	for i, row := range rows {
		items[i] = MidjourneyToDomain(row)
	}
	return items
}
func (r Reader) ListAllMidjourney(offset, limit int, query domain.MidjourneyQuery) []*domain.Midjourney {
	rows := r.Store.GetAllTasks(offset, limit, midjourneyReadQuery(query))
	if rows == nil {
		return nil
	}
	items := make([]*domain.Midjourney, len(rows))
	for i, row := range rows {
		items[i] = MidjourneyToDomain(row)
	}
	return items
}
func (r Reader) CountOwnMidjourney(userID int, query domain.MidjourneyQuery) int64 {
	return r.Store.CountAllUserTask(userID, midjourneyReadQuery(query))
}
func (r Reader) CountAllMidjourney(query domain.MidjourneyQuery) int64 {
	return r.Store.CountAllTasks(midjourneyReadQuery(query))
}
func midjourneyReadQuery(query domain.MidjourneyQuery) record.TaskQueryParams {
	return record.TaskQueryParams{ChannelID: query.ChannelID, MjID: query.MjID, StartTimestamp: query.StartTimestamp, EndTimestamp: query.EndTimestamp}
}
