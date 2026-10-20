package model

import (
	"github.com/QuantumNous/new-api/common"
	bindemail "github.com/QuantumNous/new-api/internal/accounts/bind_email"
	resetpassword "github.com/QuantumNous/new-api/internal/identity/reset_password"
	"gorm.io/gorm"
)

type PasswordResetOperation struct {
	user *User
	deps ResetDependencies
}

func (op *PasswordResetOperation) FindUnique(email string) error {
	user, err := op.deps.FindUnique(email)
	if err != nil {
		return err
	}
	op.user = user
	return nil
}
func (op *PasswordResetOperation) Hash(password string) (string, error) {
	return common.HashAccountPassword(password)
}
func (op *PasswordResetOperation) Transaction(run func(resetpassword.Mutation) error) error {
	return op.deps.DB().Transaction(func(tx *gorm.DB) error {
		return run(passwordResetMutation{user: op.user, tx: tx, advance: op.deps.Advance})
	})
}
func (op *PasswordResetOperation) PublishAuthCache() error { return op.deps.Publish(op.user.Id) }
func (op *PasswordResetOperation) RevokeSessions(reason string) error {
	_, err := op.deps.Revoke(op.user.Id, reason)
	return err
}

type passwordResetMutation struct {
	advance func(*gorm.DB, int) (int64, error)
	user    *User
	tx      *gorm.DB
}

func (m passwordResetMutation) AdvanceAuthVersion() error {
	_, err := m.advance(m.tx, m.user.Id)
	return err
}
func (m passwordResetMutation) SavePassword(hash string) error {
	return m.tx.Model(&User{}).Where("id = ?", m.user.Id).Update("password", hash).Error
}

type EmailBindingOperation struct {
	user *User
	deps EmailDependencies
}

func (op EmailBindingOperation) Normalize(email string) string { return op.deps.Normalize(email) }
func (op EmailBindingOperation) Transaction(email string, run func(bindemail.Mutation) error) error {
	return op.deps.DB().Transaction(func(tx *gorm.DB) error {
		return op.deps.WithEmailLock(tx, email, func(tx *gorm.DB) error { return run(emailBindingMutation{user: op.user, tx: tx, deps: op.deps}) })
	})
}
func (op EmailBindingOperation) UpdateCache() error { return op.deps.Cache() }

type emailBindingMutation struct {
	deps EmailDependencies
	user *User
	tx   *gorm.DB
}

func (m emailBindingMutation) EnsureAvailable(email string) error {
	return m.deps.EnsureEmail(m.tx, email, m.user.Id)
}
func (m emailBindingMutation) Assign(email string) { m.user.Email = email }
func (m emailBindingMutation) Save() error         { return m.deps.Save(m.tx, m.user) }

type ResetDependencies struct {
	DB         func() *gorm.DB
	FindUnique func(string) (*User, error)
	Advance    func(*gorm.DB, int) (int64, error)
	Publish    func(int) error
	Revoke     func(int, string) (int64, error)
}

func NewPasswordResetOperation(deps ResetDependencies) *PasswordResetOperation {
	return &PasswordResetOperation{deps: deps}
}

type EmailDependencies struct {
	DB            func() *gorm.DB
	Normalize     func(string) string
	WithEmailLock func(*gorm.DB, string, func(*gorm.DB) error) error
	EnsureEmail   func(*gorm.DB, string, int) error
	Save          func(*gorm.DB, *User) error
	Cache         func() error
}

func NewEmailBindingOperation(user *User, deps EmailDependencies) EmailBindingOperation {
	return EmailBindingOperation{user: user, deps: deps}
}
