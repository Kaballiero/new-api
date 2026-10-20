package factors

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
)

type Enable struct {
	Identity        domain.AuthIdentity
	FlowToken, Code string
	Finish          func(domain.AuthIdentity, string, string) error
	Advance         func(domain.AuthIdentity, string) (*dto.AuthBundle, error)
	Result          *dto.AuthBundle
}

func (op *Enable) FinishEnrollment() error { return op.Finish(op.Identity, op.FlowToken, op.Code) }
func (op *Enable) AdvanceSession() error {
	var err error
	op.Result, err = op.Advance(op.Identity, "twofa_enabled")
	return err
}
func (op *Enable) Bundle() *dto.AuthBundle { return op.Result }

type Disable struct {
	Identity domain.AuthIdentity
	Advance  func(domain.AuthIdentity, string) (*dto.AuthBundle, error)
	Result   *dto.AuthBundle
}

func (op *Disable) DisableFactor() error { return model.DisableTwoFAForSession(op.Identity) }
func (op *Disable) AdvanceSession() error {
	var err error
	op.Result, err = op.Advance(op.Identity, "twofa_disabled")
	return err
}
func (op *Disable) Bundle() *dto.AuthBundle { return op.Result }

type BackupCodes struct {
	Identity domain.AuthIdentity
	Advance  func(domain.AuthIdentity, string) (*dto.AuthBundle, error)
	Result   *dto.AuthBundle
	Codes    []string
}

func (op *BackupCodes) GenerateBackupCodes() error {
	var err error
	op.Codes, err = common.GenerateBackupCodes()
	return err
}
func (op *BackupCodes) ReplaceBackupCodes() error {
	return model.ReplaceBackupCodesForSession(op.Identity, op.Codes)
}
func (op *BackupCodes) AdvanceSession() error {
	var err error
	op.Result, err = op.Advance(op.Identity, "twofa_backup_codes_regenerated")
	return err
}
func (op *BackupCodes) Bundle() *dto.AuthBundle { return op.Result }
func (op *BackupCodes) BackupCodes() []string   { return op.Codes }
