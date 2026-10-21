package model

import (
	"github.com/QuantumNous/new-api/common"
	clearbinding "github.com/QuantumNous/new-api/internal/accounts/clear_binding"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	persist "github.com/QuantumNous/new-api/internal/accounts/persist_account"
	register "github.com/QuantumNous/new-api/internal/accounts/register_account"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

// Insert registers the user; lang is the language of the acting request for
// the persisted registration and invitation log texts.
func (user *User) Insert(inviterID int, lang string) error {
	operation := newRegistrationOperation(user, lang)
	return register.NewUsecase(operation, operation).Execute(inviterID, initialRegistrationAccount)
}

func initialRegistrationAccount() accountdomain.InitialAccount {
	return accountdomain.InitialAccount{Quota: common.QuotaForNewUser, AffiliateCode: common.GetRandomString(4)}
}

// FinishInsert completes a registration inserted with InsertWithTx; lang is the
// language of the acting request for the persisted log texts.
func (user *User) FinishInsert(inviterID int, lang string) {
	operation := newRegistrationOperation(user, lang)
	register.NewUsecase(operation, operation).Finish(inviterID, false)
}

func (user *User) InsertWithTx(tx *gorm.DB, inviterID int) error {
	// Inserting the defaults writes no log texts, so no language is needed.
	operation := newRegistrationOperation(user, "")
	return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		return register.NewUsecase(operation, operation).InsertDefaults(record.NewRegistrationMutation((*record.User)(user), tx, ensureEmailAvailableWithTx))
	})
}

func (user *User) FinalizeOAuthUserCreation(inviterID int, lang string) {
	operation := newRegistrationOperation(user, lang)
	register.NewUsecase(operation, operation).Finish(inviterID, true)
}

func (user *User) Edit(password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, true)).Execute(password, true)
}

func (user *User) UpdateWithTx(tx *gorm.DB, password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, false)).WithTransaction(record.NewPersistenceMutation((*record.User)(user), tx, password, false, IncrementUserAuthVersionWithTx, lockForUpdate), password, false)
}

func (user *User) EditWithTx(tx *gorm.DB, password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, false)).WithTransaction(record.NewPersistenceMutation((*record.User)(user), tx, password, true, IncrementUserAuthVersionWithTx, lockForUpdate), password, true)
}
func newRegistrationOperation(user *User, lang string) record.RegistrationOperation {
	return record.NewRegistrationOperation((*record.User)(user), lang, record.RegistrationDependencies{
		DB: func() *gorm.DB { return DB }, EnsureEmail: ensureEmailAvailableWithTx,
		WithEmailLock: withNormalizedEmailLock, Sidebar: generateDefaultSidebarConfigForRole,
		SaveSidebar:     func(value *record.User) { _ = (*User)(value).Update(false) },
		RecordSystemLog: RecordSystemLog, IncreaseUserQuota: IncreaseUserQuota, InviteUser: inviteUser,
	})
}
func newPersistenceOperation(user *User, edit bool) record.PersistenceOperation {
	return record.NewPersistenceOperation((*record.User)(user), edit, record.PersistenceDependencies{
		DB: func() *gorm.DB { return DB }, LockForUpdate: lockForUpdate, AdvanceAuthVersion: IncrementUserAuthVersionWithTx,
		UpdateCache: func() error { return updateUserCache(*user) }, RevokeSessions: func(reason string) error { _, err := RevokeAllUserSessions(user.Id, reason); return err },
	})
}

func (user *User) Update(password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, false)).Execute(password, false)
}
func (user *User) ClearBinding(bindingType string) error {
	return clearbinding.NewBindingChange(record.NewBindingOperation((*record.User)(user), func() *gorm.DB { return DB }, ReleaseExternalIdentityWithTx, ExternalIdentityProviderTelegram, func() error { return updateUserCache(*user) })).Execute(bindingType)
}
