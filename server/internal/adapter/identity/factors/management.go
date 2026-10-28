package factors

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"gorm.io/gorm"
)

type TwoFAStatus struct{ SQL identitystore.Store }

func (op TwoFAStatus) Factor(id int) (domain.FactorState, error) {
	factor, err := op.SQL.GetTwoFAByUserId(id)
	if err != nil {
		return domain.FactorState{}, err
	}
	if factor == nil {
		return domain.FactorState{}, nil
	}
	return domain.FactorState{Present: true, Enabled: factor.IsEnabled, Locked: factor.IsLocked()}, nil
}
func (op TwoFAStatus) BackupCount(id int) (int, error) { return op.SQL.GetUnusedBackupCodeCount(id) }
func (TwoFAStatus) LogCountFailure(err error) {
	common.SysLog("failed to count backup codes: " + err.Error())
}

type TwoFAStats struct{ SQL identitystore.Store }

func (op TwoFAStats) Stats() (map[string]any, error) { return op.SQL.GetTwoFAStats() }

type PasskeyStatus struct{ SQL identitystore.Store }

func (op PasskeyStatus) LastUsed(id int) (*time.Time, error) {
	credential, err := op.SQL.GetPasskeyByUserID(id)
	if err != nil {
		return nil, err
	}
	return credential.LastUsedAt, nil
}

type DeletePasskey struct {
	SQL identitystore.Store

	Identity domain.AuthIdentity
	Advance  func(domain.AuthIdentity, string) (*dto.AuthBundle, error)
	Result   *dto.AuthBundle
}

func (op *DeletePasskey) DeleteCredential() error { return op.SQL.DeletePasskeyForSession(op.Identity) }
func (op *DeletePasskey) AdvanceSession() error {
	var err error
	op.Result, err = op.Advance(op.Identity, "passkey_deleted")
	return err
}
func (op *DeletePasskey) Bundle() *dto.AuthBundle { return op.Result }

type AdminTwoFA struct {
	Accounts accountstore.Store

	SQL  identitystore.Store
	Role func() int
}

func (op AdminTwoFA) LoadTarget(id int) (int, error) {
	user, err := op.Accounts.GetUserById(id, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, domain.ErrTargetNotFound
	}
	if err != nil {
		return 0, err
	}
	return user.Role, nil
}
func (op AdminTwoFA) ActorRole() int       { return op.Role() }
func (AdminTwoFA) RootRole() int           { return common.RoleRootUser }
func (op AdminTwoFA) Disable(id int) error { return op.SQL.DisableTwoFAWithAuthVersion(id) }
func (op AdminTwoFA) RevokeAll(id int, reason string) error {
	_, err := op.SQL.RevokeAllUserSessions(id, reason)
	return err
}

type AdminPasskey struct {
	Accounts accountstore.Store

	SQL  identitystore.Store
	Role func() int
}

func (op AdminPasskey) LoadTarget(id int) (int, string, int, error) {
	user := &accountrecord.User{Id: id}
	if err := op.Accounts.FillUserById(user); err != nil {
		return 0, "", 0, err
	}
	return user.Id, user.Username, user.Role, nil
}
func (op AdminPasskey) ActorRole() int { return op.Role() }
func (AdminPasskey) RootRole() int     { return common.RoleRootUser }
func (op AdminPasskey) RequireCredential(id int) error {
	_, err := op.SQL.GetPasskeyByUserID(id)
	return err
}
func (op AdminPasskey) DeleteCredential(id int) error {
	return op.SQL.DeletePasskeyByUserIDWithAuthVersion(id)
}
func (op AdminPasskey) RevokeAll(id int, reason string) error {
	_, err := op.SQL.RevokeAllUserSessions(id, reason)
	return err
}
