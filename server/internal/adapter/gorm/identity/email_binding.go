package identity

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"gorm.io/gorm"
	"time"
)

const (
	emailBindingTTL         = 10 * time.Minute
	EmailBindingResendDelay = time.Minute
	EmailBindingMaxAttempts = 5
)

// EmailBindingState is server-owned payload. It deliberately contains only
// salted code hashes and an already-consumed authorization, never usable codes
// or proof tokens. Resending must retain the deadline and failure counter.
type EmailBindingState struct {
	Authorization  *identitydomain.FlowAuthorization `json:"authorization"`
	CurrentEmail   string                            `json:"current_email"`
	Email          string                            `json:"email"`
	NewCodeHash    string                            `json:"new_code_hash"`
	OldCodeHash    string                            `json:"old_code_hash,omitempty"`
	FailedAttempts int                               `json:"failed_attempts"`
	ResendAt       int64                             `json:"resend_at"`
}

func (s Store) CreateEmailBinding(identity identitydomain.AuthIdentity, state EmailBindingState) (string, *identityrecord.AuthFlow, error) {
	var token string
	var flow *identityrecord.AuthFlow
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.validateEmailBindingAccountWithTx(tx, identity, &state); err != nil {
			return err
		}
		if err := s.env.EnsureEmailAvailableWithTx(tx, state.Email, identity.UserID); err != nil {
			return err
		}
		payload, err := common.Marshal(state)
		if err != nil {
			return err
		}
		token, flow, err = s.createAuthFlowWithTx(tx, AuthFlowCreate{
			Purpose: identitydomain.AuthFlowPurposeEmailBinding, UserId: identity.UserID, SessionId: identity.SessionID,
			Payload: string(payload), ExpiresAt: time.Now().Add(emailBindingTTL),
		})
		return err
	})
	return token, flow, err
}

func (s Store) GetEmailBinding(identity identitydomain.AuthIdentity, token string) (*identityrecord.AuthFlow, *EmailBindingState, error) {
	flow, err := s.GetAuthFlow(token, AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeEmailBinding, UserId: identity.UserID, SessionId: identity.SessionID})
	if err != nil {
		return nil, nil, err
	}
	var state EmailBindingState
	if err := common.UnmarshalJsonStr(flow.Payload, &state); err != nil {
		return nil, nil, identitydomain.ErrAuthFlowInvalid
	}
	return flow, &state, nil
}

func (s Store) ResendEmailBinding(identity identitydomain.AuthIdentity, token, newCodeHash, oldCodeHash string) (*identityrecord.AuthFlow, *EmailBindingState, error) {
	var flow *identityrecord.AuthFlow
	var state *EmailBindingState
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		var err error
		flow, state, err = s.lockEmailBindingWithTx(tx, identity, token)
		if err != nil {
			return err
		}
		now := time.Now()
		if state.ResendAt > now.Unix() {
			return identitydomain.ErrEmailBindingResendWait
		}
		if newCodeHash == "" || (state.OldCodeHash != "") != (oldCodeHash != "") {
			return identitydomain.ErrAuthFlowInvalid
		}
		state.NewCodeHash, state.OldCodeHash = newCodeHash, oldCodeHash
		state.ResendAt = now.Add(EmailBindingResendDelay).Unix()
		payload, err := common.Marshal(state)
		if err != nil {
			return err
		}
		return tx.Model(flow).Update("payload", string(payload)).Error
	})
	return flow, state, err
}

// CompleteEmailBinding commits failed attempts even though verification fails.
// Successful confirmation consumes the flow in the same transaction as the
// email update, preserving the existing cross-database email ownership lock.
func (s Store) CompleteEmailBinding(identity identitydomain.AuthIdentity, token, email, newCode, oldCode string) (*EmailBindingState, error) {
	var state *EmailBindingState
	var verificationError error
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		// Acquire ownership protection before the first consistent read. Under
		// MySQL REPEATABLE READ, a snapshot created before this lock could hide
		// an address claimed by a concurrent transaction while we were waiting.
		if email == "" || email != s.env.NormalizeEmail(email) {
			return identitydomain.ErrAuthFlowInvalid
		}
		if err := s.env.LockNormalizedEmail(tx, email); err != nil {
			return err
		}
		flow, lockedState, err := s.lockEmailBindingWithTx(tx, identity, token)
		if err != nil {
			return err
		}
		state = lockedState
		if state.Email != email {
			return identitydomain.ErrAuthFlowInvalid
		}
		newCode, newCodeError := common.ValidateNumericCode(newCode)
		oldCode, oldCodeError := common.ValidateNumericCode(oldCode)
		newValid := newCodeError == nil && common.ValidatePasswordAndHash(newCode, state.NewCodeHash)
		oldValid := state.OldCodeHash == "" || oldCodeError == nil && common.ValidatePasswordAndHash(oldCode, state.OldCodeHash)
		if !newValid || !oldValid {
			state.FailedAttempts++
			verificationError = identitydomain.ErrEmailBindingCodeInvalid
			if state.FailedAttempts >= EmailBindingMaxAttempts {
				verificationError = identitydomain.ErrEmailBindingLocked
			}
			payload, err := common.Marshal(state)
			if err != nil {
				return err
			}
			return tx.Model(flow).Update("payload", string(payload)).Error
		}
		if err := s.env.EnsureEmailAvailableWithTx(tx, state.Email, identity.UserID); err != nil {
			return err
		}
		if err := tx.Model(&accountrecord.User{}).Where("id = ?", identity.UserID).Update("email", state.Email).Error; err != nil {
			return err
		}
		return tx.Model(flow).Update("consumed_at", time.Now()).Error
	})
	if err != nil {
		return nil, err
	}
	return state, verificationError
}

func (s Store) lockEmailBindingWithTx(tx *gorm.DB, identity identitydomain.AuthIdentity, token string) (*identityrecord.AuthFlow, *EmailBindingState, error) {
	match := AuthFlowMatch{Purpose: identitydomain.AuthFlowPurposeEmailBinding, UserId: identity.UserID, SessionId: identity.SessionID}
	// Make the first statement a write so SQLite does not have to upgrade a
	// deferred read transaction. A no-op update also locks the row on MySQL and
	// PostgreSQL; do not interpret dialect-dependent RowsAffected as success.
	if err := s.applyAuthFlowMatch(tx.Model(&identityrecord.AuthFlow{}), token, match).
		Where("consumed_at IS NULL AND expires_at > ?", time.Now()).
		UpdateColumn("payload", gorm.Expr("payload")).Error; err != nil {
		return nil, nil, err
	}
	var flow identityrecord.AuthFlow
	if err := s.applyAuthFlowMatch(tx, token, match).First(&flow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, identitydomain.ErrAuthFlowInvalid
		}
		return nil, nil, err
	}
	if flow.ConsumedAt != nil {
		return nil, nil, identitydomain.ErrAuthFlowConsumed
	}
	if !flow.ExpiresAt.After(time.Now()) {
		return nil, nil, identitydomain.ErrAuthFlowExpired
	}
	var state EmailBindingState
	if err := common.UnmarshalJsonStr(flow.Payload, &state); err != nil {
		return nil, nil, identitydomain.ErrAuthFlowInvalid
	}
	if err := s.validateEmailBindingAccountWithTx(tx, identity, &state); err != nil {
		return nil, nil, err
	}
	if state.FailedAttempts >= EmailBindingMaxAttempts {
		return nil, nil, identitydomain.ErrEmailBindingLocked
	}
	return &flow, &state, nil
}

func (s Store) validateEmailBindingAccountWithTx(tx *gorm.DB, identity identitydomain.AuthIdentity, state *EmailBindingState) error {
	if state.Authorization == nil || state.Authorization.ProofID <= 0 || state.Authorization.AuthSessionIdentity != identity ||
		state.Authorization.Scope != "account.binding.bind" || state.Authorization.ContextHash == "" ||
		state.Email == "" || state.Email != s.env.NormalizeEmail(state.Email) || state.NewCodeHash == "" {
		return identitydomain.ErrAuthFlowInvalid
	}
	if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
		return err
	}
	var user accountrecord.User
	if err := tx.Select("email").First(&user, identity.UserID).Error; err != nil {
		return err
	}
	if s.env.NormalizeEmail(user.Email) != state.CurrentEmail || state.CurrentEmail == state.Email {
		return identitydomain.ErrBindingChanged
	}
	return nil
}
