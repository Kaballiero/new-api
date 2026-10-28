package get_accounts_batch

import (
	"time"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Result struct {
	Users       []domain.UserBatchRow
	SkippedIDs  []int
	NotFoundIDs []int
}

type GetUsersBatchResponse struct {
	Users       []UserBatchRow `json:"users"`
	SkippedIDs  []int          `json:"skipped_ids"`
	NotFoundIDs []int          `json:"not_found_ids"`
}

// UserBatchRow keeps the previous batch response shape without publishing the role.
type UserBatchRow struct {
	Id           int        `json:"id"`
	Role         int        `json:"-"`
	Status       int        `json:"status"`
	Quota        int        `json:"quota"`
	UsedQuota    int        `json:"used_quota"`
	RequestCount int        `json:"request_count"`
	Group        string     `json:"group"`
	DeletedAt    *time.Time `json:"DeletedAt"`
}
