package change_group

type BulkGroupResult struct {
	Updated     int
	UpdatedIDs  []int
	SkippedIDs  []int
	NotFoundIDs []int
}

// BulkUpdateUserGroupRequest is the body of POST /api/user/group/batch.
type BulkUpdateUserGroupRequest struct {
	// UserIds is the required non-empty list of accounts to move, at most 1000 IDs.
	UserIds []int `json:"user_ids"`
	// Group is the target group; it must exist in the groupRatioMap of ratio_setting.
	Group string `json:"group"`
}

// BulkUpdateUserGroupResponse is the result of a bulk group change.
type BulkUpdateUserGroupResponse struct {
	// Updated is the number of rows affected in the database.
	Updated int `json:"updated"`
	// UpdatedIds are the IDs of updated accounts.
	UpdatedIds []int `json:"updated_ids"`
	// SkippedIds are accounts whose role the caller may not manage; they are not updated.
	SkippedIds []int `json:"skipped_ids"`
	// NotFoundIds are IDs missing in the database.
	NotFoundIds []int `json:"not_found_ids"`
}

type Input struct {
	UserIDs   []int
	Group     string
	ActorRole int
}
