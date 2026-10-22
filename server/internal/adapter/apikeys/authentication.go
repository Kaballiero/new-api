package apikeys

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// Authentication retains the shared cache reader and persistence semantics.
type Authentication struct{}

func (Authentication) Lookup(key string) (*domain.Token, error) {
	token, err := model.GetTokenByKey(key, false)
	return record.ToDomain((*record.Token)(token)), err
}
func (Authentication) UpdateStatus(token *domain.Token) error {
	return model.NewTokenPersistence().UpdateStatus(record.FromDomain(token))
}
func (Authentication) IsMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
func (Authentication) Owner(id int) (*domain.Owner, error) {
	owner, err := model.GetUserCache(id)
	if err != nil {
		return nil, err
	}
	return &domain.Owner{ID: owner.Id, Group: owner.Group, Email: owner.Email, Username: owner.Username, Setting: owner.Setting, Quota: owner.Quota, Status: owner.Status}, nil
}
func (Authentication) IsAdmin(id int) bool { return model.IsAdmin(id) }
func (Authentication) AutoGroups(token *domain.Token) ([]string, bool) {
	if token.AutoGroups == "" {
		return nil, false
	}
	var groups []string
	if err := common.UnmarshalJsonStr(token.AutoGroups, &groups); err != nil {
		common.SysError(fmt.Sprintf("failed to parse auto groups for token %d: %v", token.Id, err))
		return []string{}, true
	}
	return groups, len(groups) > 0
}
