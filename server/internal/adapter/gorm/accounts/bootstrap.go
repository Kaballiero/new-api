package accountsadapter

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type BootstrapRoot struct{}

func (BootstrapRoot) Exists() bool {
	var user model.User
	return model.DB.Where("role = ?", common.RoleRootUser).First(&user).Error == nil
}
func (BootstrapRoot) Create(username, hash string) error {
	user := model.User{Username: username, Password: hash, Role: common.RoleRootUser, Status: common.UserStatusEnabled, DisplayName: "Root User", AccessToken: nil, Quota: 100000000}
	return model.DB.Create(&user).Error
}
