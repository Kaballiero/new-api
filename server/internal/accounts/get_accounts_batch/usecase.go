package get_accounts_batch

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Reader interface {
	Batch([]int) ([]domain.UserBatchRow, error)
}
type Usecase struct {
	Reader   Reader
	RootRole int
}

// ActorRole is read after the accounts are loaded, as before the scenario was
// extracted.
type ActorRole interface {
	ActorRole() int
}

func (query Usecase) Execute(ids []int, actor ActorRole) (Result, error) {
	users, err := query.Reader.Batch(ids)
	if err != nil {
		return Result{}, domain.Internal(err)
	}
	byID := make(map[int]domain.UserBatchRow, len(users))
	for _, user := range users {
		byID[user.Id] = user
	}
	roles := make(map[int]int, len(users))
	for _, user := range users {
		roles[user.Id] = user.Role
	}
	visibleIDs, skippedIDs, notFoundIDs, err := PartitionUserBatchRoles(ids, roles, actor.ActorRole(), query.RootRole)
	visible := make([]domain.UserBatchRow, 0, len(users))
	for _, id := range visibleIDs {
		visible = append(visible, byID[id])
	}
	return Result{Users: visible, SkippedIDs: skippedIDs, NotFoundIDs: notFoundIDs}, err
}

// ParseUserBatchIDs limits the number of raw parts and keeps the first
// occurrence of each ID.
func ParseUserBatchIDs(raw string) ([]int, bool) {
	ids := make([]int, 0)
	seen := make(map[int]bool)
	count := 0
	for part := range strings.SplitSeq(raw, ",") {
		count++
		if count > 100 {
			return nil, false
		}
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id <= 0 {
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, true
}

// PartitionUserBatchRoles keeps the request order and tells missing accounts
// from accounts hidden by role.
func PartitionUserBatchRoles(ids []int, roles map[int]int, actorRole, rootRole int) (visible, skipped, missing []int, err error) {
	visible = make([]int, 0, len(roles))
	skipped = make([]int, 0)
	missing = make([]int, 0)
	for _, id := range ids {
		role, exists := roles[id]
		if !exists {
			missing = append(missing, id)
			continue
		}
		if !domain.CanManageTargetRole(actorRole, role, rootRole) {
			skipped = append(skipped, id)
			continue
		}
		visible = append(visible, id)
	}
	if len(visible) == 0 {
		if len(skipped) > 0 {
			err = domain.ErrViewForbidden
		} else {
			err = domain.ErrAccountNotFound
		}
	}
	return
}

func NewUsecase(reader Reader, rootRole int) Usecase {
	return Usecase{Reader: reader, RootRole: rootRole}
}
