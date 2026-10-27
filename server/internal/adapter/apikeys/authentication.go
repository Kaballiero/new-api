package apikeys

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	accountcache "github.com/QuantumNous/new-api/internal/adapter/cache/accounts"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"gorm.io/gorm"
)

// Authentication retains the shared cache reader and persistence semantics.
type Authentication struct {
	Reader      record.KeyReader
	Persistence record.Store
	ReadOwner   func(int) (*accountcache.UserBase, error)
	Admin       func(int) bool
}

func (a Authentication) Lookup(key string) (*domain.Token, error) {
	token, err := a.Reader.Read(key, false)
	return record.ToDomain((*record.Token)(token)), err
}
func (a Authentication) UpdateStatus(token *domain.Token) error {
	return a.Persistence.UpdateStatus(record.FromDomain(token))
}
func (a Authentication) IsMissing(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
func (a Authentication) Owner(id int) (*domain.Owner, error) {
	owner, err := a.ReadOwner(id)
	if err != nil {
		return nil, err
	}
	return &domain.Owner{ID: owner.Id, Group: owner.Group, Email: owner.Email, Username: owner.Username, Setting: owner.Setting, Quota: owner.Quota, Status: owner.Status}, nil
}
func (a Authentication) IsAdmin(id int) bool { return a.Admin(id) }
func (a Authentication) AutoGroups(token *domain.Token) ([]string, bool) {
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
