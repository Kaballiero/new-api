package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/delete_account"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"gorm.io/gorm"
)

type SoftDeletionOperation struct {
	user *User
	deps SoftDeletionDependencies
}

func (op SoftDeletionOperation) UserID() int { return op.user.Id }
func (op SoftDeletionOperation) Transaction(run func(delete_account.SoftDeletionTx) error) error {
	return op.deps.DB().Transaction(func(tx *gorm.DB) error { return run(softDeletionTx{user: op.user, tx: tx, deps: op.deps}) })
}
func (op SoftDeletionOperation) RootDeletionError() error { return op.deps.RootError() }
func (op SoftDeletionOperation) PublishAuthVersion(version int64) error {
	return op.deps.Publish(op.user.Id, version)
}
func (op SoftDeletionOperation) RevokeSessions(reason string) error {
	_, err := op.deps.Revoke(op.user.Id, reason)
	return err
}
func (op SoftDeletionOperation) InvalidateAccountCache() error {
	return op.deps.Invalidate(op.user.Id)
}

type softDeletionTx struct {
	deps SoftDeletionDependencies
	user *User
	tx   *gorm.DB
}

func (m softDeletionTx) ValidateSession(s domain.DeletionSession) error {
	return m.deps.ValidateSession(m.tx, s)
}

func (m softDeletionTx) AccountRole() (int, error) {
	var role int
	err := m.tx.Model(&User{}).Where("id = ?", m.user.Id).Select("role").Scan(&role).Error
	return role, err
}
func (m softDeletionTx) AdvanceAuthVersion() (int64, error) {
	return m.deps.Advance(m.tx, m.user.Id)
}
func (m softDeletionTx) DeleteAccount() error { return m.tx.Delete(m.user).Error }

type HardDeletionOperation struct {
	user *User
	deps HardDeletionDependencies
}

func (op *HardDeletionOperation) UserID() int { return op.user.Id }
func (op *HardDeletionOperation) Transaction(run func(delete_account.HardDeletionTx) error) error {
	return op.deps.DB().Transaction(func(tx *gorm.DB) error { return run(hardDeletionTx{operation: op, tx: tx}) })
}
func (op *HardDeletionOperation) NeedsCredentialSnapshot() bool { return common.RedisEnabled }
func (op *HardDeletionOperation) PublishAuthVersion(v int64) error {
	return op.deps.Publish(op.user.Id, v)
}
func (op *HardDeletionOperation) InvalidateCredentialCaches() error {
	return op.deps.InvalidateCredentials()
}
func (op *HardDeletionOperation) InvalidateAccountCache() error {
	return op.deps.Invalidate(op.user.Id)
}
func (op *HardDeletionOperation) LogAuthVersionFailure(e error) {
	common.SysError(fmt.Sprintf("failed to publish auth tombstone after hard deleting user %d: %v", op.user.Id, e))
}
func (op *HardDeletionOperation) LogCredentialCacheFailure(e error) {
	common.SysError(fmt.Sprintf("failed to invalidate token cache after hard deleting user %d: %v", op.user.Id, e))
}
func (op *HardDeletionOperation) LogAccountCacheFailure(e error) {
	common.SysError(fmt.Sprintf("failed to invalidate user cache after hard deleting user %d: %v", op.user.Id, e))
}

type hardDeletionTx struct {
	operation *HardDeletionOperation
	tx        *gorm.DB
}

func (m hardDeletionTx) LockAccount() error {
	var owner User
	return m.operation.deps.Lock(m.tx.Unscoped()).First(&owner, m.operation.user.Id).Error
}
func (m hardDeletionTx) UnresolvedFinancialOperations() (int64, error) {
	return m.operation.deps.FinancialDecision(m.tx, m.operation.user.Id)
}
func (m hardDeletionTx) AdvanceAuthVersion() (int64, error) {
	return m.operation.deps.Advance(m.tx, m.operation.user.Id)
}
func (m hardDeletionTx) SnapshotCredentialCaches() error {
	return m.operation.deps.SnapshotCredentials(m.tx)
}
func (m hardDeletionTx) ReleaseExternalIdentities() error {
	return m.operation.deps.ReleaseIdentities(m.tx, m.operation.user.Id)
}
func (m hardDeletionTx) RemoveAuthenticationFactors() error {
	return m.operation.deps.RemoveFactors(m.tx, m.operation.user.Id)
}
func (m hardDeletionTx) RemoveAPICredentials() error {
	return m.operation.deps.RemoveCredentials(m.tx, m.operation.user.Id)
}
func (m hardDeletionTx) RemoveProviderBindings() error {
	return m.operation.deps.RemoveBindings(m.tx, m.operation.user.Id)
}
func (m hardDeletionTx) DeleteAccountPermanently() error {
	return m.tx.Unscoped().Delete(m.operation.user).Error
}

type SoftDeletionDependencies struct {
	DB              func() *gorm.DB
	RootError       func() error
	Publish         func(int, int64) error
	Revoke          func(int, string) (int64, error)
	Invalidate      func(int) error
	ValidateSession func(*gorm.DB, domain.DeletionSession) error
	Advance         func(*gorm.DB, int) (int64, error)
}

func NewSoftDeletionOperation(user *User, deps SoftDeletionDependencies) SoftDeletionOperation {
	return SoftDeletionOperation{user: user, deps: deps}
}

type HardDeletionDependencies struct {
	Conflict                                                            func() error
	DB                                                                  func() *gorm.DB
	Lock                                                                func(*gorm.DB) *gorm.DB
	Publish                                                             func(int, int64) error
	InvalidateCredentials                                               func() error
	Invalidate                                                          func(int) error
	Advance                                                             func(*gorm.DB, int) (int64, error)
	FinancialDecision                                                   func(*gorm.DB, int) (int64, error)
	SnapshotCredentials                                                 func(*gorm.DB) error
	ReleaseIdentities, RemoveFactors, RemoveCredentials, RemoveBindings func(*gorm.DB, int) error
}

func NewHardDeletionOperation(user *User, deps HardDeletionDependencies) *HardDeletionOperation {
	return &HardDeletionOperation{user: user, deps: deps}
}

func (op *HardDeletionOperation) FinancialConflictError() error { return op.deps.Conflict() }
