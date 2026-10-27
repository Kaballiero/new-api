package logincompletion

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/QuantumNous/new-api/internal/app/identitycore"
	"github.com/QuantumNous/new-api/internal/identity/dto"
)

type Operation struct {
	Primary  *model.User
	Metadata func() (method, ip, userAgent string)
	Profile  *model.User
	Bundle   *dto.AuthBundle
}

func (op *Operation) PrimaryValid() bool {
	return op.Primary != nil && op.Primary.Id > 0 && op.Primary.Status == common.UserStatusEnabled
}
func (op *Operation) LoadProfile() error {
	var err error
	op.Profile, err = model.GetSelfUserById(op.Primary.Id)
	return err
}
func (op *Operation) CreateSession(expected int64) error {
	userID := op.Primary.Id
	method, ip, userAgent := op.Metadata()
	var err error
	if expected > 0 {
		op.Bundle, err = identitycore.CreateLoginSessionAtAuthVersion(userID, expected, method, ip, userAgent)
	} else {
		op.Bundle, err = identitycore.CreateLoginSession(userID, method, ip, userAgent)
	}
	return err
}
