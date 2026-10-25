package system

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"gorm.io/gorm"
)

type Store struct{ DB func() *gorm.DB }

func (s Store) generateSystemTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "systask_" + key, nil
}

func (s Store) createSystemTask(taskType string, payload any, state any) (*record.SystemTask, error) {
	taskID, err := s.generateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, err
	}
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return nil, err
	}

	task := &record.SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		Status:    domain.SystemTaskStatusPending,
		ActiveKey: &taskType,
		Payload:   payloadText,
		State:     stateText,
	}

	if err := s.DB().Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func (s Store) getSystemTaskByTaskID(taskID string) (*record.SystemTask, error) {
	var task record.SystemTask
	if err := s.DB().Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func (s Store) getActiveSystemTask(taskType string) (*record.SystemTask, error) {
	var task record.SystemTask
	err := s.DB().Where("type = ? AND status IN ?", taskType, activeSystemTaskStatuses()).
		Order("id desc").
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func (s Store) findEarliestPendingSystemTasks(taskTypes []string) (map[string]*record.SystemTask, error) {
	tasksByType := map[string]*record.SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := s.DB().Model(&record.SystemTask{}).
		Select("MIN(id)").
		Where("type IN ? AND status = ?", taskTypes, domain.SystemTaskStatusPending).
		Group("type")
	var tasks []*record.SystemTask
	if err := s.DB().Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func (s Store) listSystemTasks(limit int) ([]*record.SystemTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var tasks []*record.SystemTask
	err := s.DB().Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

func (s Store) getLatestSystemTasks(taskTypes []string) (map[string]*record.SystemTask, error) {
	tasksByType := map[string]*record.SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := s.DB().Model(&record.SystemTask{}).
		Select("MAX(id)").
		Where("type IN ?", taskTypes).
		Group("type")
	var tasks []*record.SystemTask
	if err := s.DB().Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func (s Store) claimSystemTask(id int64, taskType string, runnerID string, lockUntil int64) (*record.SystemTask, bool, error) {
	now := common.GetTimestamp()
	var task record.SystemTask
	if err := s.DB().Where("id = ? AND type = ? AND status = ?", id, taskType, domain.SystemTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	acquired, expiredTaskID, err := s.acquireSystemTaskLock(taskType, task.TaskID, runnerID, now, lockUntil)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	if expiredTaskID != "" && expiredTaskID != task.TaskID {
		if err := s.MarkSystemTaskLeaseExpired(expiredTaskID); err != nil {
			_ = s.ReleaseSystemTaskLock(task.TaskID, runnerID)
			return nil, false, err
		}
	}

	result := s.DB().Model(&record.SystemTask{}).
		Where("id = ? AND type = ? AND status = ?", id, taskType, domain.SystemTaskStatusPending).
		Updates(map[string]any{
			"status":     domain.SystemTaskStatusRunning,
			"locked_by":  runnerID,
			"updated_at": now,
		})
	if result.Error != nil {
		_ = s.ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		_ = s.ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, nil
	}

	if err := s.DB().Where("id = ?", id).First(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func (s Store) acquireSystemTaskLock(taskType string, taskID string, lockedBy string, now int64, lockUntil int64) (bool, string, error) {
	lock := &record.SystemTaskLock{
		Type:        taskType,
		TaskID:      taskID,
		LockedBy:    lockedBy,
		LockedUntil: lockUntil,
		UpdatedAt:   now,
	}
	if err := s.DB().Create(lock).Error; err == nil {
		return true, "", nil
	}

	var existing record.SystemTaskLock
	err := s.DB().Where("type = ?", taskType).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, "", nil
		}
		return false, "", err
	}
	if existing.LockedUntil >= now {
		return false, "", nil
	}

	result := s.DB().Model(&record.SystemTaskLock{}).
		Where("type = ? AND locked_until < ?", taskType, now).
		Updates(map[string]any{
			"task_id":      taskID,
			"locked_by":    lockedBy,
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, "", result.Error
	}
	if result.RowsAffected == 0 {
		return false, "", nil
	}
	return true, existing.TaskID, nil
}

func (s Store) UpdateSystemTaskState(taskID string, lockedBy string, state any) error {
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := s.DB().Model(&record.SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, domain.SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Updates(map[string]any{
			"state":      stateText,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	// MySQL counts changed rows, not matched rows. A no-op persist of the same
	// state in the same second therefore returns RowsAffected == 0 even while
	// the lease is still held. Confirm the lock before treating this as loss.
	// Reuse `now` from the UPDATE so a clock tick cannot reintroduce false
	// lock-loss; a lease that expires during the write is caught by the next heartbeat.
	var held int64
	err = s.DB().Model(&record.SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, domain.SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Count(&held).Error
	if err != nil {
		return err
	}
	if held == 0 {
		return domain.ErrSystemTaskLockLost
	}
	return nil
}

func (s Store) RenewSystemTaskLock(taskID string, lockedBy string, lockUntil int64) error {
	now := common.GetTimestamp()
	result := s.DB().Model(&record.SystemTaskLock{}).
		Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
		Updates(map[string]any{
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrSystemTaskLockLost
	}
	return nil
}

func (s Store) MarkSystemTaskLeaseExpired(taskID string) error {
	result := s.DB().Model(&record.SystemTask{}).
		Where("task_id = ? AND status = ?", taskID, domain.SystemTaskStatusRunning).
		Updates(map[string]any{
			"status":     domain.SystemTaskStatusFailed,
			"active_key": nil,
			"error":      "task lease expired",
			"updated_at": common.GetTimestamp(),
		})
	return result.Error
}

func (s Store) ExpireStaleSystemTaskLocks(now int64) error {
	var locks []*record.SystemTaskLock
	if err := s.DB().Where("locked_until < ?", now).Find(&locks).Error; err != nil {
		return err
	}
	for _, lock := range locks {
		if err := s.MarkSystemTaskLeaseExpired(lock.TaskID); err != nil {
			return err
		}
		result := s.DB().Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until < ?", lock.Type, lock.TaskID, lock.LockedBy, now).
			Delete(&record.SystemTaskLock{})
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

func (s Store) ReleaseSystemTaskLock(taskID string, lockedBy string) error {
	result := s.DB().Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&record.SystemTaskLock{})
	return result.Error
}

func (s Store) FinishSystemTask(taskID string, lockedBy string, status domain.SystemTaskStatus, resultPayload any, errorMessage string) error {
	resultText, err := marshalSystemTaskJSON(resultPayload)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	result := s.DB().Model(&record.SystemTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, domain.SystemTaskStatusRunning, lockedBy).
		Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now).
		Updates(map[string]any{
			"status":     status,
			"active_key": nil,
			"result":     resultText,
			"error":      errorMessage,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrSystemTaskLockLost
	}
	return s.ReleaseSystemTaskLock(taskID, lockedBy)
}

func activeSystemTaskStatuses() []string {
	return []string{string(domain.SystemTaskStatusPending), string(domain.SystemTaskStatusRunning)}
}

func marshalSystemTaskJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSystemTaskJSONString(data string, v any) error {
	if data == "" {
		return nil
	}
	return common.UnmarshalJsonStr(data, v)
}

func decodeSystemTaskJSONValue(data string) any {
	if data == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(data, &value); err != nil {
		return data
	}
	return value
}
