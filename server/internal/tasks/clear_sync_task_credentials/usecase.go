package clear_sync_task_credentials

import "context"

// pageSize bounds the synchronous Tasks read per page.
const pageSize = 200

// Store pages synchronous Tasks and removes the credentials stored in them.
type Store interface {
	ClearOpenRouterSyncCredentials(afterID int64, limit int) (lastID int64, cleared int, err error)
}

// Usecase removes the account credentials that earlier releases stored in
// synchronous Tasks. A synchronous Task authenticates its cost lookup with the
// bound channel account, so no stored credential is needed.
type Usecase struct {
	store Store
}

func NewUsecase(store Store) Usecase {
	return Usecase{store: store}
}

// Execute returns how many Tasks were cleared; a repeated run clears none.
func (u Usecase) Execute(ctx context.Context) (int, error) {
	total := 0
	for afterID := int64(0); ; {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		lastID, cleared, err := u.store.ClearOpenRouterSyncCredentials(afterID, pageSize)
		total += cleared
		if err != nil || lastID == afterID {
			return total, err
		}
		afterID = lastID
	}
}
