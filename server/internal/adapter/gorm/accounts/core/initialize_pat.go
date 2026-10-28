package core

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/initialize_pat"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"

	"gorm.io/gorm"
)

func (store Store) Transaction(id int, run func(initialize_pat.Account) error) error {
	return store.env.DB().Transaction(func(tx *gorm.DB) error {
		var user record.User
		if err := locking.LockForUpdate(tx).First(&user, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domain.ErrGetAPIAccountNotFound
			}
			return domain.ErrGetAPICredentialUnavailable
		}
		return run(patAccount{tx: tx, user: &user})
	})
}

type patAccount struct {
	tx   *gorm.DB
	user *record.User
}

func (a patAccount) Snapshot() initialize_pat.Snapshot {
	return initialize_pat.Snapshot{ID: a.user.Id, Role: a.user.Role, Status: a.user.Status, Username: a.user.Username, AccessToken: a.user.GetAccessToken()}
}
func (a patAccount) SavePAT(token string, now int64) error {
	return a.tx.Model(a.user).Updates(map[string]any{"access_token": token, "access_token_created_at": now}).Error
}
