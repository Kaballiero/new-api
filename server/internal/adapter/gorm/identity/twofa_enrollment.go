package identity

import (
	"crypto/hmac"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	locking "github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"

	"gorm.io/gorm"
)

type twoFAEnrollmentPayload struct {
	TwoFAID       int                               `json:"twofa_id"`
	SecretHash    string                            `json:"secret_hash"`
	Authorization *identitydomain.FlowAuthorization `json:"authorization"`
}

// CreateTwoFAEnrollment stores the pending credential, recovery codes and
// session-bound flow atomically. Reinitialization invalidates the previous setup.
func (s Store) CreateTwoFAEnrollment(identity identitydomain.AuthIdentity, authorization *identitydomain.FlowAuthorization, secret string, backupCodes []string, expiresAt time.Time) (string, error) {
	if authorization == nil || authorization.ProofID <= 0 || authorization.AuthSessionIdentity != identity {
		return "", identitydomain.ErrTwoFASetupInvalid
	}
	var token string
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var existing identityrecord.TwoFA
		err := locking.LockForUpdate(tx).Where("user_id = ?", identity.UserID).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if existing.IsEnabled {
				return identitydomain.ErrTwoFAAlreadyEnabled
			}
			if err := tx.Unscoped().Delete(&existing).Error; err != nil {
				return err
			}
		}
		pending := &identityrecord.TwoFA{UserId: identity.UserID, Secret: secret}
		if err := tx.Create(pending).Error; err != nil {
			return err
		}
		if err := s.replaceBackupCodesWithTx(tx, identity.UserID, backupCodes); err != nil {
			return err
		}
		payload, err := common.Marshal(twoFAEnrollmentPayload{
			TwoFAID: pending.Id, SecretHash: common.GenerateHMACWithKey([]byte("twofa-enrollment:"+common.SessionSecret), secret),
			Authorization: authorization,
		})
		if err != nil {
			return err
		}
		token, _, err = s.createAuthFlowWithTx(tx, AuthFlowCreate{
			Purpose: identitydomain.AuthFlowPurposeTwoFASetup, UserId: identity.UserID, SessionId: identity.SessionID,
			Payload: string(payload), ExpiresAt: expiresAt,
		})
		return err
	})
	return token, err
}

// EnableTwoFAEnrollment validates and consumes one setup in the same transaction
// as factor activation and auth_version advancement. A bad code can be retried.
func (s Store) EnableTwoFAEnrollment(identity identitydomain.AuthIdentity, token, code string) error {
	cleanCode, err := common.ValidateNumericCode(code)
	if err != nil {
		return identitydomain.ErrTwoFACodeInvalid
	}
	_, err = s.ConsumeAuthFlowWithAction(token, AuthFlowMatch{
		Purpose: identitydomain.AuthFlowPurposeTwoFASetup, UserId: identity.UserID, SessionId: identity.SessionID,
	}, func(tx *gorm.DB, flow *identityrecord.AuthFlow) error {
		var payload twoFAEnrollmentPayload
		if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil {
			return err
		}
		if payload.Authorization == nil || payload.Authorization.ProofID <= 0 || payload.Authorization.AuthSessionIdentity != identity {
			return identitydomain.ErrTwoFASetupInvalid
		}
		if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var pending identityrecord.TwoFA
		if err := locking.LockForUpdate(tx).Where("id = ? AND user_id = ? AND is_enabled = ?", payload.TwoFAID, identity.UserID, false).First(&pending).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identitydomain.ErrTwoFASetupInvalid
			}
			return err
		}
		secretHash := common.GenerateHMACWithKey([]byte("twofa-enrollment:"+common.SessionSecret), pending.Secret)
		if !hmac.Equal([]byte(payload.SecretHash), []byte(secretHash)) {
			return identitydomain.ErrTwoFASetupInvalid
		}
		if !common.ValidateTOTPCode(pending.Secret, cleanCode) {
			return identitydomain.ErrTwoFACodeInvalid
		}
		if _, err := s.env.IncrementUserAuthVersionWithTx(tx, identity.UserID); err != nil {
			return err
		}
		return tx.Model(&pending).Updates(map[string]any{"is_enabled": true, "failed_attempts": 0, "locked_until": nil}).Error
	})
	if errors.Is(err, identitydomain.ErrAuthFlowInvalid) || errors.Is(err, identitydomain.ErrAuthFlowExpired) || errors.Is(err, identitydomain.ErrAuthFlowConsumed) {
		return identitydomain.ErrTwoFASetupInvalid
	}
	if err != nil {
		return err
	}
	return s.env.PublishUserAuthCache(identity.UserID)
}
