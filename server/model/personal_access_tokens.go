package model

import (
	"github.com/QuantumNous/new-api/common"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/personal_access_tokens"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/get_status"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/revoke_token"
	"gorm.io/gorm"
)

// NewPersonalAccessTokenPersistence preserves replaceable host DB handles and lock semantics.
func NewPersonalAccessTokenPersistence() repository.Store {
	return repository.NewStore(func() *gorm.DB { return DB }, func() *gorm.DB { return LOG_DB.Model(&AuditLog{}) }, lockForUpdate, common.GetTimestamp)
}

// UpdateUserAccessToken writes only the current PAT generation fields.
func UpdateUserAccessToken(id int, token string) error {
	return NewPersonalAccessTokenPersistence().Update(id, token)
}

// RevokeUserAccessToken returns the generation revoked under the row lock.
func RevokeUserAccessToken(id int) (string, error) {
	output, err := revoke_token.NewUsecase(NewPersonalAccessTokenPersistence(), domain.Fingerprint, nil).Execute(revoke_token.Input{UserID: id})
	return output.TokenRef, err
}

type UserAccessTokenStatus = get_status.Output

func GetUserAccessTokenStatus(id int) (*UserAccessTokenStatus, error) {
	roles := domain.AuditRoles{Root: common.RoleRootUser, Common: common.RoleCommonUser, Admin: common.RoleAdminUser}
	return get_status.NewUsecase(NewPersonalAccessTokenPersistence(), domain.Fingerprint, roles).Execute(get_status.Input{UserID: id})
}
