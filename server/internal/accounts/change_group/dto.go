package change_group

type BulkGroupResult struct {
	Updated     int
	UpdatedIDs  []int
	SkippedIDs  []int
	NotFoundIDs []int
}

// BulkUpdateUserGroupRequest — тело запроса POST /api/user/group/batch.
type BulkUpdateUserGroupRequest struct {
	// UserIds — непустой обязательный список пользователей для смены группы, не более 1000 ID.
	UserIds []int `json:"user_ids"`
	// Group — имя целевой группы, которая должна существовать в groupRatioMap пакета ratio_setting.
	Group string `json:"group"`
}

// BulkUpdateUserGroupResponse содержит результат массовой смены группы.
type BulkUpdateUserGroupResponse struct {
	// Updated — количество строк, затронутых обновлением в БД.
	Updated int `json:"updated"`
	// UpdatedIds — ID обновлённых пользователей.
	UpdatedIds []int `json:"updated_ids"`
	// SkippedIds — ID пользователей, чьей ролью вызывающий управлять не вправе; эти записи не обновлены.
	SkippedIds []int `json:"skipped_ids"`
	// NotFoundIds — ID, отсутствующие в БД.
	NotFoundIds []int `json:"not_found_ids"`
}

type Input struct {
	UserIDs   []int
	Group     string
	ActorRole int
}
