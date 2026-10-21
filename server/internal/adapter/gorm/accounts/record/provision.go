package model

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/internal/accounts/provision_account"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewProvisionOperation предоставляет существующую транзакцию через узкий порт сценария.

type provisionOperation struct {
	db     func() *gorm.DB
	ensure func(*gorm.DB, string, int) error
	insert func(*gorm.DB, *User) error
}

func (op provisionOperation) WithAccount(request provision_account.NewAccount, provision func(provision_account.ProvisionAccount) error) error {
	return op.db().Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		account := provisionAccount{tx: tx, ensure: op.ensure, insert: op.insert, user: User{Username: request.Username, Password: request.Password, DisplayName: request.DisplayName, Group: request.Group, Role: request.Role, Status: request.Status}}
		return provision(&account)
	})
}

type provisionAccount struct {
	ensure func(*gorm.DB, string, int) error
	insert func(*gorm.DB, *User) error
	tx     *gorm.DB
	user   User
}

func (account *provisionAccount) EnsureUsername() error {
	return account.ensure(account.tx, account.user.Username, 0)
}
func (account *provisionAccount) SetCredential(token string, now int64) {
	account.user.AccessToken = &token
	account.user.AccessTokenCreatedAt = &now
}
func (account *provisionAccount) SetSidebar(sidebar string) {
	setting := account.user.GetSetting()
	setting.SidebarModules = sidebar
	account.user.SetSetting(setting)
}
func (account *provisionAccount) Insert() (userID int, role int, err error) {
	if err := account.insert(account.tx, &account.user); err != nil {
		return 0, 0, err
	}
	return account.user.Id, account.user.Role, nil
}

func (account *provisionAccount) SetInitialQuota(quota int) error {
	account.user.Quota = quota
	if err := account.tx.Model(&account.user).Update("quota", account.user.Quota).Error; err != nil {
		return err
	}
	return nil
}

func (account *provisionAccount) Quota() int { return account.user.Quota }

type ProvisionCredentials struct{}

func (ProvisionCredentials) NewProvisionedPAT() (string, int64, error) {
	token, err := common.GenerateRandomKey(32)
	if err != nil {
		return "", 0, err
	}
	return token, common.GetTimestamp(), nil
}

func NewProvisionOperation(db func() *gorm.DB, ensure func(*gorm.DB, string, int) error, insert func(*gorm.DB, *User) error) provision_account.ProvisionOperation {
	return provisionOperation{db: db, ensure: ensure, insert: insert}
}
