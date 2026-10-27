package authorizationcore

import (
	"github.com/QuantumNous/new-api/common"
	casbinadapter "github.com/QuantumNous/new-api/internal/adapter/casbin/authorization"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/authorization"
	"github.com/QuantumNous/new-api/internal/authorization/check_permission"
	"github.com/QuantumNous/new-api/internal/authorization/clear_user_permissions_in_tx"
	"github.com/QuantumNous/new-api/internal/authorization/domain"
	"github.com/QuantumNous/new-api/internal/authorization/get_capabilities"
	"github.com/QuantumNous/new-api/internal/authorization/initialize_authorization"
	"github.com/QuantumNous/new-api/internal/authorization/reload_policy"
	"github.com/QuantumNous/new-api/internal/authorization/set_user_permissions"
	"github.com/QuantumNous/new-api/internal/authorization/set_user_permissions_in_tx"
	"github.com/QuantumNous/new-api/internal/authorization/sync_policy"
	"gorm.io/gorm"
	"sync"
	"time"
)

type Permission = domain.Permission
type PermissionsMap = domain.PermissionsMap

var (
	AuditRead             = Permission{Resource: domain.ResourceAudit, Action: domain.ActionRead}
	ChannelRead           = Permission{Resource: domain.ResourceChannel, Action: domain.ActionRead}
	ChannelOperate        = Permission{Resource: domain.ResourceChannel, Action: domain.ActionOperate}
	ChannelWrite          = Permission{Resource: domain.ResourceChannel, Action: domain.ActionWrite}
	ChannelSensitiveWrite = Permission{Resource: domain.ResourceChannel, Action: domain.ActionSensitiveWrite}
	TaskPluginBind        = Permission{Resource: domain.ResourceTaskPlugin, Action: domain.ActionBind}
	// One policy state is shared by startup, middleware and account mutations.
	registry            = domain.BuiltInResources()
	builtInRoles        = domain.BuiltInRoles()
	state               casbinadapter.State
	resolveSubjectRoles = func(userID, systemRole int) []string {
		return domain.ResolveSystemRoles(systemRole, common.RoleRootUser, common.RoleAdminUser)
	}
)

func currentCatalog() domain.Catalog {
	return domain.Catalog{Resources: registry, BuiltInRoles: builtInRoles}
}
func Catalog() []domain.ResourceDefinition {
	return currentCatalog().ResourcesCopy()
}
func Roles() []domain.RoleDescriptor { return currentCatalog().Roles() }
func readPolicy() domain.PolicyReader {
	p := casbinadapter.NewPolicy(state.Current())
	if p == nil {
		return nil
	}
	return p
}
func writePolicy() set_user_permissions.Policy {
	p := casbinadapter.NewPolicy(state.Current())
	if p == nil {
		return nil
	}
	return p
}
func accessRules() domain.AccessRules {
	return domain.AccessRules{Catalog: currentCatalog, ResolveRoles: func(id, role int) []string { return resolveSubjectRoles(id, role) }, Policy: readPolicy}
}
func Can(id, role int, permission Permission) bool {
	return operations().check.Execute(id, role, permission)
}
func Capabilities(id, role int) PermissionsMap {
	return operations().capabilities.Execute(id, role)
}
func SetUserPermissions(id int, permissions PermissionsMap) error {
	return operations().set.Execute(id, permissions)
}
func SetUserPermissionsInTx(tx *gorm.DB, id int, permissions PermissionsMap) error {
	return set_user_permissions_in_tx.NewUsecase(currentCatalog, readPolicy, repository.NewStore(tx)).Execute(id, permissions)
}
func ClearUserAuthorizationInTx(tx *gorm.DB, id int) error {
	return clear_user_permissions_in_tx.NewUsecase(currentCatalog, repository.NewStore(tx)).Execute(id)
}
func Init(db *gorm.DB) error {
	engine := casbinadapter.NewEngine(&state, repository.NewPolicyAdapter(db))
	return initialize_authorization.NewUsecase(currentCatalog, func() bool { return common.IsMasterNode }, repository.NewStore(db), engine).Execute()
}
func ReloadPolicy() error { return operations().reload.Execute() }
func StartPolicySync(frequency int) {
	operations().sync.Execute(frequency)
}

type runtime struct {
	check        check_permission.Usecase
	capabilities get_capabilities.Usecase
	set          set_user_permissions.Usecase
	reload       reload_policy.Usecase
	sync         sync_policy.Usecase
}

var operations = sync.OnceValue(func() runtime {
	return runtime{
		check:        check_permission.NewUsecase(accessRules()),
		capabilities: get_capabilities.NewUsecase(currentCatalog, accessRules()),
		set:          set_user_permissions.NewUsecase(currentCatalog, writePolicy),
		reload:       reload_policy.NewUsecase(state.Reload),
		sync:         sync_policy.NewUsecase(state.Reload, time.Sleep, common.SysError),
	}
})
