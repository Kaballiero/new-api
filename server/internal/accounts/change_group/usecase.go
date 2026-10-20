package change_group

import (
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type GroupStore interface {
	RolesByIDs([]int) (map[int]int, error)
	UpdateGroup([]int, string) (int64, error)
}

type Usecase struct {
	Store       GroupStore
	GroupExists func(string) bool
	RootRole    int
}

func (change Usecase) Execute(input Input) (BulkGroupResult, error) {
	ids, group, myRole := input.UserIDs, input.Group, input.ActorRole
	if len(ids) == 0 || group == "" {
		return BulkGroupResult{}, accounts.ErrInvalidGroupChange
	}
	if len(ids) > accounts.MaxBulkGroupBatch {
		return BulkGroupResult{}, accounts.ErrGroupBatchTooLarge
	}
	if !change.GroupExists(group) {
		return BulkGroupResult{}, accounts.ErrGroupDoesNotExist
	}
	roles, err := change.Store.RolesByIDs(ids)
	if err != nil {
		return BulkGroupResult{}, err
	}
	allowed := make([]int, 0, len(ids))
	skipped := make([]int, 0)
	notFound := make([]int, 0)
	for _, id := range ids {
		targetRole, exists := roles[id]
		if !exists {
			notFound = append(notFound, id)
			continue
		}
		if !domain.CanManageTargetRole(myRole, targetRole, change.RootRole) {
			skipped = append(skipped, id)
			continue
		}
		allowed = append(allowed, id)
	}
	if len(allowed) == 0 {
		switch {
		case len(notFound) == len(ids):
			return BulkGroupResult{}, accounts.ErrGroupUsersNotFound
		case len(skipped) > 0:
			return BulkGroupResult{}, accounts.ErrGroupChangeForbidden
		default:
			return BulkGroupResult{}, accounts.ErrInvalidGroupChange
		}
	}
	affected, err := change.Store.UpdateGroup(allowed, group)
	if err != nil {
		return BulkGroupResult{}, err
	}
	return BulkGroupResult{Updated: int(affected), UpdatedIDs: allowed, SkippedIDs: skipped, NotFoundIDs: notFound}, nil
}

func NewUsecase(store GroupStore, exists func(string) bool, rootRole int) Usecase {
	return Usecase{Store: store, GroupExists: exists, RootRole: rootRole}
}

type GroupMutation interface {
	Update([]int, string) (int64, error)
	Commit() error
	Rollback()
}
type GroupPersistence interface {
	SQLite() bool
	Begin() GroupMutation
	RefreshEnabled() bool
	RefreshGroups([]int)
}
type GroupUpdate struct{ store GroupPersistence }

func NewGroupUpdate(store GroupPersistence) GroupUpdate { return GroupUpdate{store: store} }
func (u GroupUpdate) Execute(ids []int, group string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	const sqliteChunk = 500
	chunks := [][]int{ids}
	if u.store.SQLite() && len(ids) > sqliteChunk {
		chunks = chunks[:0]
		for i := 0; i < len(ids); i += sqliteChunk {
			chunks = append(chunks, ids[i:min(i+sqliteChunk, len(ids))])
		}
	}
	var affected int64
	tx := u.store.Begin()
	for _, chunk := range chunks {
		count, err := tx.Update(chunk, group)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		affected += count
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if u.store.RefreshEnabled() {
		u.store.RefreshGroups(ids)
	}
	return affected, nil
}
