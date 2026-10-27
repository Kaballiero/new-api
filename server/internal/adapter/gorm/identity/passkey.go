package identity

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	locking "github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"
	"time"
)

// ErrFriendlyPasskeyNotFound hides storage failures of a passkey lookup. It
// never reaches a response: identity transports write it as a generic error.
var ErrFriendlyPasskeyNotFound = errors.New("passkey lookup failed")

// errPasskeySave reports a rejected or failed passkey write; like other storage
// failures it is written as a generic error response.
var errPasskeySave = errors.New("passkey save failed")

func (s Store) GetPasskeyByUserID(userID int) (*identityrecord.PasskeyCredential, error) {
	if userID == 0 {
		common.SysLog("GetPasskeyByUserID: empty user ID")
		return nil, ErrFriendlyPasskeyNotFound
	}
	var credential identityrecord.PasskeyCredential
	if err := s.env.DB().Where("user_id = ?", userID).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// A missing record is expected (no passkey is bound): return ErrPasskeyNotFound without logging.
			return nil, identitydomain.ErrPasskeyNotFound
		}
		// Only real database errors are logged.
		common.SysLog(fmt.Sprintf("GetPasskeyByUserID: database error for user %d: %v", userID, err))
		return nil, ErrFriendlyPasskeyNotFound
	}
	return &credential, nil
}

func (s Store) GetPasskeyByCredentialID(credentialID []byte) (*identityrecord.PasskeyCredential, error) {
	if len(credentialID) == 0 {
		common.SysLog("GetPasskeyByCredentialID: empty credential ID")
		return nil, ErrFriendlyPasskeyNotFound
	}

	credIDStr := base64.StdEncoding.EncodeToString(credentialID)
	var credential identityrecord.PasskeyCredential
	if err := s.env.DB().Where("credential_id = ?", credIDStr).First(&credential).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysLog(fmt.Sprintf("GetPasskeyByCredentialID: passkey not found for credential ID length %d", len(credentialID)))
			return nil, ErrFriendlyPasskeyNotFound
		}
		common.SysLog(fmt.Sprintf("GetPasskeyByCredentialID: database error for credential ID: %v", err))
		return nil, ErrFriendlyPasskeyNotFound
	}

	return &credential, nil
}

// UpdatePasskeyAssertionState persists only fields produced by a successful
// assertion. Registration identity (credential ID, public key, AAGUID,
// transports and attestation metadata) is immutable on this path.
func (s Store) UpdatePasskeyAssertionState(userID int, credential *webauthn.Credential, lastUsedAt time.Time) error {
	if userID <= 0 || credential == nil || len(credential.ID) == 0 || lastUsedAt.IsZero() {
		return errPasskeySave
	}
	credentialID := base64.StdEncoding.EncodeToString(credential.ID)
	result := s.env.DB().Model(&identityrecord.PasskeyCredential{}).
		Where("user_id = ? AND credential_id = ?", userID, credentialID).
		Updates(map[string]any{
			"sign_count":      credential.Authenticator.SignCount,
			"clone_warning":   credential.Authenticator.CloneWarning,
			"user_present":    credential.Flags.UserPresent,
			"user_verified":   credential.Flags.UserVerified,
			"backup_eligible": credential.Flags.BackupEligible,
			"backup_state":    credential.Flags.BackupState,
			"last_used_at":    lastUsedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return identitydomain.ErrPasskeyNotFound
	}
	return nil
}

func (s Store) upsertPasskeyCredentialWithTx(tx *gorm.DB, credential *identityrecord.PasskeyCredential) error {
	if err := tx.Unscoped().Where("user_id = ?", credential.UserID).Delete(&identityrecord.PasskeyCredential{}).Error; err != nil {
		common.SysLog(fmt.Sprintf("UpsertPasskeyCredential: failed to delete existing credential for user %d: %v", credential.UserID, err))
		return errPasskeySave
	}
	if err := tx.Create(credential).Error; err != nil {
		common.SysLog(fmt.Sprintf("UpsertPasskeyCredential: failed to create credential for user %d: %v", credential.UserID, err))
		return errPasskeySave
	}
	return nil
}

// UpsertPasskeyCredentialWithAuthVersion is reserved for enrollment changes;
// assertion sign-count updates must use UpdatePasskeyAssertionState.
func (s Store) UpsertPasskeyCredentialWithAuthVersion(credential *identityrecord.PasskeyCredential) error {
	return s.upsertPasskeyCredentialWithAuthVersion(credential, nil)
}

func (s Store) RegisterPasskeyForSession(identity identitydomain.AuthIdentity, credential *identityrecord.PasskeyCredential) error {
	return s.upsertPasskeyCredentialWithAuthVersion(credential, &identity)
}

func (s Store) upsertPasskeyCredentialWithAuthVersion(credential *identityrecord.PasskeyCredential, identity *identitydomain.AuthIdentity) error {
	if credential == nil || credential.UserID <= 0 {
		return errPasskeySave
	}
	if err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if identity != nil {
			if identity.UserID != credential.UserID {
				return identitydomain.ErrSessionInactive
			}
			if err := s.ValidateAuthSessionWithTx(tx, *identity); err != nil {
				return err
			}
		}
		if _, err := s.env.IncrementUserAuthVersionWithTx(tx, credential.UserID); err != nil {
			return err
		}
		return s.upsertPasskeyCredentialWithTx(tx, credential)
	}); err != nil {
		return err
	}
	return s.env.PublishUserAuthCache(credential.UserID)
}

func (s Store) DeletePasskeyByUserIDWithAuthVersion(userID int) error {
	return s.deletePasskeyWithAuthVersion(userID, nil)
}

func (s Store) DeletePasskeyForSession(identity identitydomain.AuthIdentity) error {
	return s.deletePasskeyWithAuthVersion(identity.UserID, &identity)
}

func (s Store) deletePasskeyWithAuthVersion(userID int, identity *identitydomain.AuthIdentity) error {
	if userID == 0 {
		return errors.New("passkey deletion requires a user id")
	}
	if err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if identity != nil {
			if err := s.ValidateAuthSessionWithTx(tx, *identity); err != nil {
				return err
			}
		}
		var credential identityrecord.PasskeyCredential
		if err := locking.LockForUpdate(tx).Where("user_id = ?", userID).First(&credential).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identitydomain.ErrPasskeyNotFound
			}
			return err
		}
		if _, err := s.env.IncrementUserAuthVersionWithTx(tx, userID); err != nil {
			return err
		}
		result := tx.Unscoped().Delete(&credential)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return identitydomain.ErrPasskeyNotFound
		}
		return nil
	}); err != nil {
		return err
	}
	return s.env.PublishUserAuthCache(userID)
}
