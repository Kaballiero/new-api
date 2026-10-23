package payments

import (
	"github.com/QuantumNous/new-api/internal/adapter/wallet"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/model"
)

type Users struct{}

func (Users) Group(userID int) (string, error) { return model.GetUserGroup(userID, true) }

func (Users) Find(userID int) (*domain.User, error) {
	user, err := model.GetUserById(userID, false)
	if user == nil {
		return nil, err
	}
	return &domain.User{ID: user.Id, Email: user.Email, Username: user.Username, Group: user.Group, StripeCustomer: user.StripeCustomer}, err
}

type Wallet struct{}

func (Wallet) Capacity(userID, quota int) error { return walletError(wallet.Capacity(userID, quota)) }
