package model

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"gorm.io/gorm"
)

func newSoftDeletionOperation(user *User) record.SoftDeletionOperation {
	return record.NewSoftDeletionOperation((*record.User)(user), record.SoftDeletionDependencies{
		DB: func() *gorm.DB { return DB }, RootError: func() error { return ErrCannotDeleteRootUser }, Publish: publishCommittedUserAuthVersion, Revoke: RevokeAllUserSessions, Invalidate: invalidateUserCache, Advance: IncrementUserAuthVersionWithTx,
		ValidateSession: func(tx *gorm.DB, s domain.DeletionSession) error {
			return ValidateAuthSessionWithTx(tx, AuthSessionIdentity{UserID: s.UserID, SessionID: s.SessionID, UserAuthVersion: s.UserAuthVersion, SessionVersion: s.SessionVersion})
		},
	})
}
func newHardDeletionOperation(user *User) *record.HardDeletionOperation {
	var tokens []Token
	return record.NewHardDeletionOperation((*record.User)(user), record.HardDeletionDependencies{
		DB: func() *gorm.DB { return DB }, Lock: lockForUpdate, Publish: publishCommittedUserAuthVersion, Invalidate: invalidateUserCache, Advance: IncrementUserAuthVersionWithTx,
		InvalidateCredentials: func() error { return invalidateTokensCache(tokens) },
		SnapshotCredentials: func(tx *gorm.DB) error {
			return tx.Unscoped().Select("id", commonKeyCol).Where("user_id = ?", user.Id).Find(&tokens).Error
		},
		Conflict: func() error { return ErrBatchConflict }, FinancialDecision: accountDeletionFinancialDecision, ReleaseIdentities: releaseAllExternalIdentitiesWithTx, RemoveFactors: removeAccountAuthenticationFactors,
		RemoveCredentials: func(tx *gorm.DB, id int) error { return tx.Unscoped().Where("user_id = ?", id).Delete(&Token{}).Error }, RemoveBindings: deleteUserOAuthBindingsByUserId,
	})
}

// These primitives retain their existing cross-module ORM owners.
func accountDeletionFinancialDecision(tx *gorm.DB, id int) (int64, error) {
	var unresolved int64
	err := tx.Model(&BatchJob{}).Where("user_id = ? AND (reserved_quota > 0 OR billing_status <> ? OR purchase_status <> ?)", id, "settled", "settled").Count(&unresolved).Error
	return unresolved, err
}

func removeAccountAuthenticationFactors(tx *gorm.DB, id int) error {
	for _, data := range []any{&TwoFABackupCode{}, &TwoFA{}, &UserSession{}, &AuthFlow{}, &PasskeyCredential{}} {
		if err := tx.Unscoped().Where("user_id = ?", id).Delete(data).Error; err != nil {
			return err
		}
	}
	return nil
}
