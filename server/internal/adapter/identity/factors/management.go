package factors

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"time"
)

type TwoFAStatus struct{}

func (TwoFAStatus) Factor(id int) (domain.FactorState, error) {
	factor, err := model.GetTwoFAByUserId(id)
	if err != nil {
		return domain.FactorState{}, err
	}
	if factor == nil {
		return domain.FactorState{}, nil
	}
	return domain.FactorState{Present: true, Enabled: factor.IsEnabled, Locked: factor.IsLocked()}, nil
}
func (TwoFAStatus) BackupCount(id int) (int, error) { return model.GetUnusedBackupCodeCount(id) }
func (TwoFAStatus) LogCountFailure(err error) {
	common.SysLog("获取备用码数量失败: " + err.Error())
}

type TwoFAStats struct{}

func (TwoFAStats) Stats() (map[string]any, error) { return model.GetTwoFAStats() }

type PasskeyStatus struct{}

func (PasskeyStatus) LastUsed(id int) (*time.Time, error) {
	credential, err := model.GetPasskeyByUserID(id)
	if err != nil {
		return nil, err
	}
	return credential.LastUsedAt, nil
}

type DeletePasskey struct {
	Identity domain.AuthIdentity
	Advance  func(domain.AuthIdentity, string) (*dto.AuthBundle, error)
	Result   *dto.AuthBundle
}

func (op *DeletePasskey) DeleteCredential() error { return model.DeletePasskeyForSession(op.Identity) }
func (op *DeletePasskey) AdvanceSession() error {
	var err error
	op.Result, err = op.Advance(op.Identity, "passkey_deleted")
	return err
}
func (op *DeletePasskey) Bundle() *dto.AuthBundle { return op.Result }

type AdminTwoFA struct{ Role func() int }

func (AdminTwoFA) LoadTarget(id int) (int, error) {
	user, err := model.GetUserById(id, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, domain.ErrTargetNotFound
	}
	if err != nil {
		return 0, err
	}
	return user.Role, nil
}
func (op AdminTwoFA) ActorRole() int    { return op.Role() }
func (AdminTwoFA) RootRole() int        { return common.RoleRootUser }
func (AdminTwoFA) Disable(id int) error { return model.DisableTwoFAWithAuthVersion(id) }
func (AdminTwoFA) RevokeAll(id int, reason string) error {
	_, err := model.RevokeAllUserSessions(id, reason)
	return err
}

type AdminPasskey struct{ Role func() int }

func (AdminPasskey) LoadTarget(id int) (int, string, int, error) {
	user := &model.User{Id: id}
	if err := user.FillUserById(); err != nil {
		return 0, "", 0, err
	}
	return user.Id, user.Username, user.Role, nil
}
func (op AdminPasskey) ActorRole() int { return op.Role() }
func (AdminPasskey) RootRole() int     { return common.RoleRootUser }
func (AdminPasskey) RequireCredential(id int) error {
	_, err := model.GetPasskeyByUserID(id)
	return err
}
func (AdminPasskey) DeleteCredential(id int) error {
	return model.DeletePasskeyByUserIDWithAuthVersion(id)
}
func (AdminPasskey) RevokeAll(id int, reason string) error {
	_, err := model.RevokeAllUserSessions(id, reason)
	return err
}
