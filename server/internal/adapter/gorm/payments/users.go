package payments

import (
	accountsql "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	"github.com/QuantumNous/new-api/internal/adapter/wallet"
	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Users struct{ Accounts accountsql.Store }

func (u Users) Group(userID int) (string, error) { return u.Accounts.GetUserGroup(userID, true) }

func (u Users) Find(userID int) (*domain.User, error) {
	user, err := u.Accounts.GetUserById(userID, false)
	if user == nil {
		return nil, err
	}
	return &domain.User{ID: user.Id, Email: user.Email, Username: user.Username, Group: user.Group, StripeCustomer: user.StripeCustomer}, err
}

type Wallet struct{}

func (Wallet) Capacity(userID, quota int) error { return walletError(wallet.Capacity(userID, quota)) }
