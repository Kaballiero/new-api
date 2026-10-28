package logincompletion

import (
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/identity/dto"
)

type Operation struct {
	Create          func(int, string, string, string) (*dto.AuthBundle, error)
	CreateAtVersion func(int, int64, string, string, string) (*dto.AuthBundle, error)

	Accounts accountstore.Store

	Primary  *accountrecord.User
	Metadata func() (method, ip, userAgent string)
	Profile  *accountrecord.User
	Bundle   *dto.AuthBundle
}

func (op *Operation) PrimaryValid() bool {
	return op.Primary != nil && op.Primary.Id > 0 && op.Primary.Status == common.UserStatusEnabled
}
func (op *Operation) LoadProfile() error {
	var err error
	op.Profile, err = op.Accounts.GetSelfUserById(op.Primary.Id)
	return err
}
func (op *Operation) CreateSession(expected int64) error {
	userID := op.Primary.Id
	method, ip, userAgent := op.Metadata()
	var err error
	if expected > 0 {
		op.Bundle, err = op.CreateAtVersion(userID, expected, method, ip, userAgent)
	} else {
		op.Bundle, err = op.Create(userID, method, ip, userAgent)
	}
	return err
}
