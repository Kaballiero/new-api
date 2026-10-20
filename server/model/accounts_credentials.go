package model

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

func newPasswordResetOperation() *record.PasswordResetOperation {
	return record.NewPasswordResetOperation(record.ResetDependencies{DB: func() *gorm.DB { return DB }, FindUnique: func(email string) (*record.User, error) {
		user, err := GetUniqueUserByEmail(email)
		return (*record.User)(user), err
	}, Advance: IncrementUserAuthVersionWithTx, Publish: PublishUserAuthCache, Revoke: RevokeAllUserSessions})
}
func newEmailBindingOperation(user *User) record.EmailBindingOperation {
	return record.NewEmailBindingOperation((*record.User)(user), record.EmailDependencies{DB: func() *gorm.DB { return DB }, Normalize: NormalizeEmail, WithEmailLock: withNormalizedEmailLock, EnsureEmail: ensureEmailAvailableWithTx, Save: func(tx *gorm.DB, value *record.User) error { return (*User)(value).UpdateWithTx(tx, false) }, Cache: func() error { return updateUserCache(*user) }})
}
