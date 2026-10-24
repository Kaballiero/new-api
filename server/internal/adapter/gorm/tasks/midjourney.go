package tasks

import "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"

func (s Store) GetAllUserTask(userId int, startIdx int, num int, queryParams record.TaskQueryParams) []*record.Midjourney {
	var tasks []*record.Midjourney
	var err error

	// Build the existing filtered query.
	query := s.db().Where("user_id = ?", userId)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// Keep timestamp filtering in the existing storage representation.
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// Read the requested page.
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func (s Store) GetAllTasks(startIdx int, num int, queryParams record.TaskQueryParams) []*record.Midjourney {
	var tasks []*record.Midjourney
	var err error

	// Build the existing filtered query.
	query := s.db()

	// Apply the existing optional filters.
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// Read the requested page.
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func (s Store) GetAllUnFinishTasks() []*record.Midjourney {
	var tasks []*record.Midjourney
	var err error
	// get all tasks progress is not 100%
	err = s.db().Where("progress != ?", "100%").Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

// HasUnfinishedMidjourneyTasks reports whether at least one record.Midjourney task is
// still in progress. It is a cheap existence check (LIMIT 1) used to decide
// whether the midjourney_poll system task needs to run; when no task is pending
// the scheduler skips creating a row entirely.
func (s Store) HasUnfinishedMidjourneyTasks() bool {
	var id int
	err := s.db().Model(&record.Midjourney{}).
		Where("progress != ?", "100%").
		Limit(1).
		Pluck("id", &id).Error
	return err == nil && id != 0
}

func (s Store) GetByOnlyMJId(mjId string) *record.Midjourney {
	var mj *record.Midjourney
	var err error
	err = s.db().Where("mj_id = ?", mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func (s Store) GetByMJId(userId int, mjId string) *record.Midjourney {
	var mj *record.Midjourney
	var err error
	err = s.db().Where("user_id = ? and mj_id = ?", userId, mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func (s Store) GetByMJIds(userId int, mjIds []string) []*record.Midjourney {
	var mj []*record.Midjourney
	var err error
	err = s.db().Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func (s Store) InsertMidjourney(midjourney *record.Midjourney) error {
	var err error
	err = s.db().Create(midjourney).Error
	return err
}

func (s Store) UpdateMidjourney(midjourney *record.Midjourney) error {
	var err error
	err = s.db().Save(midjourney).Error
	return err
}

func (s Store) UpdateMidjourneyBillingState(midjourney *record.Midjourney) error {
	return s.db().Model(midjourney).
		Select("quota", "token_id", "billing_channel_id").
		Updates(midjourney).Error
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Uses Model().Select("*").Updates() to avoid GORM Save()'s INSERT fallback.
func (s Store) UpdateMidjourneyWithStatus(midjourney *record.Midjourney, fromStatus string) (bool, error) {
	result := s.db().Model(midjourney).Where("status = ?", fromStatus).Select("*").Updates(midjourney)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s Store) MjBulkUpdate(mjIds []string, params map[string]any) error {
	return s.db().Model(&record.Midjourney{}).
		Where("mj_id in (?)", mjIds).
		Updates(params).Error
}

func (s Store) MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	return s.db().Model(&record.Midjourney{}).
		Where("id in (?)", taskIDs).
		Updates(params).Error
}

// CountAllTasks returns total midjourney tasks for admin query
func (s Store) CountAllTasks(queryParams record.TaskQueryParams) int64 {
	var total int64
	query := s.db().Model(&record.Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func (s Store) CountAllUserTask(userId int, queryParams record.TaskQueryParams) int64 {
	var total int64
	query := s.db().Model(&record.Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
