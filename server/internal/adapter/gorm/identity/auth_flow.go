package identity

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	locking "github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

const (
	authFlowTokenBytes              = 32
	authFlowDefaultCleanupRetention = 24 * time.Hour
)

type AuthFlowCreate struct {
	Purpose   string
	Provider  string
	Intent    string
	UserId    int
	SessionId string
	Payload   string
	ExpiresAt time.Time
}

type AuthFlowMatch struct {
	Purpose   string
	Provider  string
	Intent    string
	UserId    int
	SessionId string
}

// ValidateAuthSessionWithTx rechecks the authoritative identity while holding the
// user/session locks until the caller's credential change or flow consumption commits.
func (s Store) ValidateAuthSessionWithTx(tx *gorm.DB, identity identitydomain.AuthIdentity) error {
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 {
		return identitydomain.ErrSessionInactive
	}
	var user accountrecord.User
	if err := locking.LockForUpdate(tx).First(&user, identity.UserID).Error; err != nil {
		return err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion != identity.UserAuthVersion {
		return identitydomain.ErrSessionInactive
	}
	var session identityrecord.UserSession
	if err := locking.LockForUpdate(tx).Where("sid = ? AND user_id = ?", identity.SessionID, identity.UserID).First(&session).Error; err != nil {
		return err
	}
	if session.Status != identityrecord.UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= time.Now().Unix() || session.UserAuthVersion != identity.UserAuthVersion || session.Version != identity.SessionVersion {
		return identitydomain.ErrSessionInactive
	}
	return nil
}

func (s Store) applyAuthFlowMatch(query *gorm.DB, token string, match AuthFlowMatch) *gorm.DB {
	query = query.Where("token_hash = ? AND purpose = ?", s.authFlowTokenHash(token), match.Purpose)
	if match.Provider != "" {
		query = query.Where("provider = ?", match.Provider)
	}
	if match.Intent != "" {
		query = query.Where("intent = ?", match.Intent)
	}
	if match.UserId != 0 {
		query = query.Where("user_id = ?", match.UserId)
	}
	if match.SessionId != "" {
		query = query.Where("session_id = ?", match.SessionId)
	}
	return query
}

func (s Store) authFlowTokenHash(token string) string {
	return common.GenerateHMACWithKey([]byte("auth-flow-v1:"+common.SessionSecret), token)
}

func (s Store) CreateAuthFlow(input AuthFlowCreate) (string, *identityrecord.AuthFlow, error) {
	return s.createAuthFlowWithTx(s.env.DB(), input)
}

func (s Store) createAuthFlowWithTx(tx *gorm.DB, input AuthFlowCreate) (string, *identityrecord.AuthFlow, error) {
	if strings.TrimSpace(input.Purpose) == "" || input.ExpiresAt.IsZero() || !input.ExpiresAt.After(time.Now()) {
		return "", nil, identitydomain.ErrAuthFlowInvalid
	}
	random := make([]byte, authFlowTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", nil, fmt.Errorf("generate auth flow token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	flow := &identityrecord.AuthFlow{
		TokenHash: s.authFlowTokenHash(token),
		Purpose:   input.Purpose,
		Provider:  input.Provider,
		Intent:    input.Intent,
		UserId:    input.UserId,
		SessionId: input.SessionId,
		Payload:   input.Payload,
		ExpiresAt: input.ExpiresAt,
	}
	if err := tx.Create(flow).Error; err != nil {
		return "", nil, err
	}
	return token, flow, nil
}

// ClaimExternalAuthAssertion records a signed provider assertion as consumed.
// The assertion is HMACed before storage and the unique token_hash index makes
// replay rejection atomic on SQLite, MySQL and PostgreSQL.
func (s Store) ClaimExternalAuthAssertion(purpose, assertion string, expiresAt time.Time) error {
	return s.env.DB().Transaction(func(tx *gorm.DB) error {
		return s.ClaimExternalAuthAssertionWithTx(tx, purpose, assertion, expiresAt)
	})
}

// ClaimExternalAuthAssertionWithTx records a provider assertion in the
// caller's transaction so replay protection can commit atomically with the
// authentication flow and its resulting state change.
func (s Store) ClaimExternalAuthAssertionWithTx(tx *gorm.DB, purpose, assertion string, expiresAt time.Time) error {
	purpose = strings.TrimSpace(purpose)
	assertion = strings.TrimSpace(assertion)
	now := time.Now()
	if tx == nil || purpose == "" || assertion == "" || !expiresAt.After(now) {
		return identitydomain.ErrAuthFlowInvalid
	}
	flow := identityrecord.AuthFlow{
		TokenHash:  s.authFlowTokenHash("external:" + purpose + ":" + assertion),
		Purpose:    purpose,
		ExpiresAt:  expiresAt,
		ConsumedAt: &now,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_hash"}},
		DoNothing: true,
	}).Create(&flow)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return identitydomain.ErrAuthFlowConsumed
	}
	return nil
}

// GetAuthFlow validates a flow without consuming it. Callers must still use
// ConsumeAuthFlow with all identity-bound fields before performing the action.
func (s Store) GetAuthFlow(token string, match AuthFlowMatch) (*identityrecord.AuthFlow, error) {
	if token == "" || match.Purpose == "" {
		return nil, identitydomain.ErrAuthFlowInvalid
	}
	var flow identityrecord.AuthFlow
	if err := s.applyAuthFlowMatch(s.env.DB(), token, match).First(&flow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, identitydomain.ErrAuthFlowInvalid
		}
		return nil, err
	}
	if flow.ConsumedAt != nil {
		return nil, identitydomain.ErrAuthFlowConsumed
	}
	if !flow.ExpiresAt.After(time.Now()) {
		return nil, identitydomain.ErrAuthFlowExpired
	}
	return &flow, nil
}

// ConsumeAuthFlow atomically validates and consumes a flow. Optional match
// fields are enforced when non-zero so tokens cannot cross purposes or users.
func (s Store) ConsumeAuthFlow(token string, match AuthFlowMatch) (*identityrecord.AuthFlow, error) {
	return s.ConsumeAuthFlowWithAction(token, match, nil)
}

// ConsumeAuthFlowWithAction consumes a flow and runs action in the same
// database transaction. An action failure rolls the consumption back.
func (s Store) ConsumeAuthFlowWithAction(token string, match AuthFlowMatch, action func(tx *gorm.DB, flow *identityrecord.AuthFlow) error) (*identityrecord.AuthFlow, error) {
	if token == "" || match.Purpose == "" {
		return nil, identitydomain.ErrAuthFlowInvalid
	}
	var consumed identityrecord.AuthFlow
	err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		// Claim with the first write, rather than upgrading a prior read lock.
		// SQLite cannot reliably upgrade two concurrent deferred read transactions.
		now := time.Now()
		result := s.applyAuthFlowMatch(tx.Model(&identityrecord.AuthFlow{}), token, match).
			Where("consumed_at IS NULL AND expires_at > ?", now).
			Update("consumed_at", now)
		if result.Error != nil {
			return result.Error
		}
		query := s.applyAuthFlowMatch(tx, token, match)
		if err := query.First(&consumed).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identitydomain.ErrAuthFlowInvalid
			}
			return err
		}
		if result.RowsAffected != 1 && consumed.ConsumedAt != nil {
			return identitydomain.ErrAuthFlowConsumed
		}
		if !consumed.ExpiresAt.After(time.Now()) {
			return identitydomain.ErrAuthFlowExpired
		}
		if result.RowsAffected != 1 {
			return identitydomain.ErrAuthFlowInvalid
		}
		if action != nil {
			if err := action(tx, &consumed); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &consumed, nil
}

func (s Store) DeleteExpiredAuthFlows(now time.Time) error {
	cutoff := now.Add(-authFlowDefaultCleanupRetention)
	return s.env.DB().Where("expires_at < ? OR (consumed_at IS NOT NULL AND consumed_at < ?)", cutoff, cutoff).
		Delete(&identityrecord.AuthFlow{}).Error
}
