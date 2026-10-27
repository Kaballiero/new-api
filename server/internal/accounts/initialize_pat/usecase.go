package initialize_pat

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"strings"
)

type Account interface {
	Snapshot() Snapshot
	SavePAT(string, int64) error
}
type Store interface {
	Transaction(int, func(Account) error) error
}
type Usecase struct {
	store                         Store
	token                         func() (string, error)
	now                           func() int64
	commonRole, enabled, disabled int
}

func NewUsecase(store Store, token func() (string, error), now func() int64, commonRole, enabled, disabled int) Usecase {
	return Usecase{store: store, token: token, now: now, commonRole: commonRole, enabled: enabled, disabled: disabled}
}
func (u Usecase) Execute(request Input) (*Result, error) {
	var result *Result
	err := u.store.Transaction(request.UserID, func(account Account) error {
		user := account.Snapshot()
		if user.Role != u.commonRole {
			return domain.ErrGetAPITargetRoleDenied
		}
		if user.Username != request.ExpectedUsername {
			return domain.ErrGetAPIUsernameMismatch
		}
		if user.Status != u.enabled && user.Status != u.disabled {
			return domain.ErrGetAPICredentialUnavailable
		}
		state := "active"
		if user.Status == u.disabled {
			state = "blocked"
		}
		token := strings.TrimRight(user.AccessToken, " ")
		if token != "" {
			outcome := "would_reuse"
			if request.Apply {
				outcome = "reused"
			}
			result = &Result{UserID: user.ID, State: state, Outcome: outcome}
			if request.Apply {
				result.AccessToken = &token
			}
			return nil
		}
		if state == "blocked" {
			result = &Result{UserID: user.ID, State: state, Outcome: "blocked_no_pat"}
			return nil
		}
		result = &Result{UserID: user.ID, State: state, Outcome: "would_issue"}
		if !request.Apply {
			return nil
		}
		newToken, err := u.token()
		if err != nil {
			return domain.ErrGetAPICredentialUnavailable
		}
		now := u.now()
		if err := account.SavePAT(newToken, now); err != nil {
			return domain.ErrGetAPICredentialUnavailable
		}
		result.Outcome = "issued"
		result.AccessToken = &newToken
		return nil
	})
	return result, err
}
