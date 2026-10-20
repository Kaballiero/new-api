package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts"
	"github.com/QuantumNous/new-api/internal/accounts/provision_account"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

type ProvisionCredentials = record.ProvisionCredentials

func ProvisionGetAPIUser(principalRole int, request accounts.GetAPICreateUserRequest) (*accounts.GetAPICreateCredential, error) {
	return provision_account.NewUsecase(NewProvisionOperation(), ProvisionCredentials{}, ProvisionErrors{}, ratio_setting.ContainsGroupRatio, generateDefaultSidebarConfigForRole, common.MaxQuota, common.RoleCommonUser, common.UserStatusEnabled).Execute(principalRole, request)
}

type ProvisionErrors struct{}

func (ProvisionErrors) IsUsernameConflict(err error) bool { return isUsernameUniqueViolation(err) }

func (ProvisionErrors) Invalid() error       { return ErrGetAPIInvalidRequest }
func (ProvisionErrors) UsernameTaken() error { return ErrUsernameAlreadyTaken }
func (ProvisionErrors) Conflict() error      { return ErrGetAPICreateConflict }
func (ProvisionErrors) Unavailable() error   { return ErrGetAPICredentialUnavailable }
func (ProvisionErrors) Denied() error        { return ErrGetAPICapabilityDenied }

func NewProvisionOperation() provision_account.ProvisionOperation {
	return record.NewProvisionOperation(func() *gorm.DB { return DB }, EnsureUsernameAvailableWithTx, func(tx *gorm.DB, user *record.User) error { return (*User)(user).InsertWithTx(tx, 0) })
}
