package clear_sync_task_credentials

import (
	"context"

	"github.com/QuantumNous/new-api/internal/tasks/domain"
)

// pageSize bounds the synchronous Tasks read per page.
const pageSize = 200

// Store pages synchronous Tasks and removes the credentials stored in them,
// binding an unfinished Task to its account with bind before its key is
// dropped.
type Store interface {
	ClearOpenRouterSyncCredentials(afterID int64, limit int, bind func(task *domain.Task, key string) error) (lastID int64, cleared int, err error)
}

// Accounts binds a Task stored before account binding to the channel account
// of its stored key.
type Accounts interface {
	Bind(task *domain.Task, key string) error
}

// Usecase removes the account credentials that earlier releases stored in
// synchronous Tasks. A synchronous Task authenticates its cost lookup with the
// bound channel account, so no stored credential is needed.
type Usecase struct {
	store    Store
	accounts Accounts
}

func NewUsecase(store Store, accounts Accounts) Usecase {
	return Usecase{store: store, accounts: accounts}
}

// Execute returns how many Tasks were cleared; a repeated run clears none.
func (u Usecase) Execute(ctx context.Context) (int, error) {
	total := 0
	for afterID := int64(0); ; {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		lastID, cleared, err := u.store.ClearOpenRouterSyncCredentials(afterID, pageSize, u.accounts.Bind)
		total += cleared
		if err != nil || lastID == afterID {
			return total, err
		}
		afterID = lastID
	}
}
