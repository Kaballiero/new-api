package delete_account

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

type DeletionSubject interface{ UserID() int }

// DeletionSessionSource reads the identity inside the transaction; nil means the
// previous entry point without a session.
type DeletionSessionSource func() domain.DeletionSession

type SoftDeletionTx interface {
	ValidateSession(domain.DeletionSession) error
	AccountRole() (int, error)
	AdvanceAuthVersion() (int64, error)
	DeleteAccount() error
}
type SoftDeletionOperation interface {
	DeletionSubject
	Transaction(func(SoftDeletionTx) error) error
	RootDeletionError() error
	PublishAuthVersion(int64) error
	RevokeSessions(reason string) error
	InvalidateAccountCache() error
}

type HardDeletionTx interface {
	LockAccount() error
	UnresolvedFinancialOperations() (int64, error)
	AdvanceAuthVersion() (int64, error)
	SnapshotCredentialCaches() error
	ReleaseExternalIdentities() error
	RemoveAuthenticationFactors() error
	RemoveAPICredentials() error
	RemoveProviderBindings() error
	DeleteAccountPermanently() error
}
type HardDeletionOperation interface {
	DeletionSubject
	Transaction(func(HardDeletionTx) error) error
	NeedsCredentialSnapshot() bool
	FinancialConflictError() error
	PublishAuthVersion(int64) error
	InvalidateCredentialCaches() error
	InvalidateAccountCache() error
	LogAuthVersionFailure(error)
	LogCredentialCacheFailure(error)
	LogAccountCacheFailure(error)
}

// SoftDeleteAccount keeps the transactional session check and stops the chain
// on a failure after commit.
func SoftDeleteAccount(operation SoftDeletionOperation, source DeletionSessionSource, rootRole int) error {
	if operation.UserID() == 0 {
		return errors.New("id 为空！")
	}
	var nextAuthVersion int64
	if err := operation.Transaction(func(tx SoftDeletionTx) error {
		if source != nil {
			if err := tx.ValidateSession(source()); err != nil {
				return err
			}
			role, err := tx.AccountRole()
			if err != nil {
				return err
			}
			if role == rootRole {
				return operation.RootDeletionError()
			}
		}
		var err error
		nextAuthVersion, err = tx.AdvanceAuthVersion()
		if err != nil {
			return err
		}
		return tx.DeleteAccount()
	}); err != nil {
		return err
	}
	if err := operation.PublishAuthVersion(nextAuthVersion); err != nil {
		return err
	}
	if err := operation.RevokeSessions("user_deleted"); err != nil {
		return err
	}
	return operation.InvalidateAccountCache()
}

// HardDeleteAccount coordinates the shared transaction and keeps cleaning caches
// after commit when a step fails.
func HardDeleteAccount(operation HardDeletionOperation) error {
	if operation.UserID() == 0 {
		return errors.New("id 为空！")
	}
	var deletedAuthVersion int64
	err := operation.Transaction(func(tx HardDeletionTx) error {
		if err := tx.LockAccount(); err != nil {
			return err
		}
		unresolved, err := tx.UnresolvedFinancialOperations()
		if err != nil {
			return err
		}
		if unresolved > 0 {
			return operation.FinancialConflictError()
		}

		deletedAuthVersion, err = tx.AdvanceAuthVersion()
		if err != nil {
			return err
		}
		if operation.NeedsCredentialSnapshot() {
			if err := tx.SnapshotCredentialCaches(); err != nil {
				return err
			}
		}
		if err := tx.ReleaseExternalIdentities(); err != nil {
			return err
		}
		if err := tx.RemoveAuthenticationFactors(); err != nil {
			return err
		}
		if err := tx.RemoveAPICredentials(); err != nil {
			return err
		}
		if err := tx.RemoveProviderBindings(); err != nil {
			return err
		}
		return tx.DeleteAccountPermanently()
	})
	if err != nil {
		return err
	}
	if err := operation.PublishAuthVersion(deletedAuthVersion); err != nil {
		operation.LogAuthVersionFailure(err)
	}
	if err := operation.InvalidateCredentialCaches(); err != nil {
		operation.LogCredentialCacheFailure(err)
	}
	if err := operation.InvalidateAccountCache(); err != nil {
		operation.LogAccountCacheFailure(err)
	}
	return nil
}
