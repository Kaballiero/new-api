package get_accounts_batch

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type Reader interface {
	Batch([]int) ([]domain.UserBatchRow, error)
}
type Usecase struct {
	Reader   Reader
	RootRole int
}

// ActorRole читается после загрузки пользователей, как и до выделения сценария.
type ActorRole interface {
	ActorRole() int
}

func (query Usecase) Execute(ids []int, actor ActorRole) (Result, *accounts.AdminFailure) {
	users, err := query.Reader.Batch(ids)
	if err != nil {
		return Result{}, &accounts.AdminFailure{Kind: accounts.AdminLoadFailed, Cause: err}
	}
	byID := make(map[int]domain.UserBatchRow, len(users))
	for _, user := range users {
		byID[user.Id] = user
	}
	roles := make(map[int]int, len(users))
	for _, user := range users {
		roles[user.Id] = user.Role
	}
	visibleIDs, skippedIDs, notFoundIDs, failure := PartitionUserBatchRoles(ids, roles, actor.ActorRole(), query.RootRole)
	visible := make([]domain.UserBatchRow, 0, len(users))
	for _, id := range visibleIDs {
		visible = append(visible, byID[id])
	}
	return Result{Users: visible, SkippedIDs: skippedIDs, NotFoundIDs: notFoundIDs}, failure
}

// ParseUserBatchIDs ограничивает число исходных частей и сохраняет первое вхождение каждого ID.
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

// PartitionUserBatchRoles сохраняет порядок запроса и различает отсутствующие и недоступные по правам записи.
func PartitionUserBatchRoles(ids []int, roles map[int]int, actorRole, rootRole int) (visible, skipped, missing []int, failure *accounts.AdminFailure) {
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
			failure = &accounts.AdminFailure{Kind: accounts.AdminForbidden}
		} else {
			failure = &accounts.AdminFailure{Kind: accounts.AdminLoadMissing}
		}
	}
	return
}

func NewUsecase(reader Reader, rootRole int) Usecase {
	return Usecase{Reader: reader, RootRole: rootRole}
}
