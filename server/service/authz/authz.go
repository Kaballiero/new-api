package authz

import (
	"github.com/QuantumNous/new-api/common"
	casbinadapter "github.com/QuantumNous/new-api/internal/adapter/casbin/authorization"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/authorization"
	"github.com/QuantumNous/new-api/internal/authorization/check_permission"
	"github.com/QuantumNous/new-api/internal/authorization/clear_user_permissions"
	"github.com/QuantumNous/new-api/internal/authorization/clear_user_permissions_in_tx"
	"github.com/QuantumNous/new-api/internal/authorization/domain"
	"github.com/QuantumNous/new-api/internal/authorization/get_capabilities"
	"github.com/QuantumNous/new-api/internal/authorization/get_permission_catalog"
	"github.com/QuantumNous/new-api/internal/authorization/get_user_overrides"
	"github.com/QuantumNous/new-api/internal/authorization/initialize_authorization"
	"github.com/QuantumNous/new-api/internal/authorization/reload_policy"
	"github.com/QuantumNous/new-api/internal/authorization/set_user_permissions"
	"github.com/QuantumNous/new-api/internal/authorization/set_user_permissions_in_tx"
	"github.com/QuantumNous/new-api/internal/authorization/sync_policy"
	"github.com/casbin/casbin/v2"
	"gorm.io/gorm"
	"sync"
	"time"
)

type Permission = domain.Permission
type PermissionsMap = domain.PermissionsMap
type RoleSpec = domain.RoleSpec
type ActionDefinition = get_permission_catalog.ActionDefinition
type ResourceDefinition = get_permission_catalog.ResourceDefinition
type RoleDescriptor = get_permission_catalog.RoleDescriptor

const (
	EffectAllow          = domain.EffectAllow
	EffectDeny           = domain.EffectDeny
	BuiltInRoleRoot      = domain.BuiltInRoleRoot
	BuiltInRoleAdmin     = domain.BuiltInRoleAdmin
	ResourceAudit        = domain.ResourceAudit
	ResourceChannel      = domain.ResourceChannel
	ResourceTaskPlugin   = domain.ResourceTaskPlugin
	ActionRead           = domain.ActionRead
	ActionOperate        = domain.ActionOperate
	ActionWrite          = domain.ActionWrite
	ActionSensitiveWrite = domain.ActionSensitiveWrite
	ActionSecretView     = domain.ActionSecretView
	ActionBind           = domain.ActionBind
)

var (
	AuditRead             = Permission{Resource: ResourceAudit, Action: ActionRead}
	ChannelRead           = Permission{Resource: ResourceChannel, Action: ActionRead}
	ChannelOperate        = Permission{Resource: ResourceChannel, Action: ActionOperate}
	ChannelWrite          = Permission{Resource: ResourceChannel, Action: ActionWrite}
	ChannelSensitiveWrite = Permission{Resource: ResourceChannel, Action: ActionSensitiveWrite}
	ChannelSecretView     = Permission{Resource: ResourceChannel, Action: ActionSecretView}
	TaskPluginBind        = Permission{Resource: ResourceTaskPlugin, Action: ActionBind}
	// Retained legacy state; no new singleton or service locator is introduced.
	registry            = domain.BuiltInResources()
	builtInRoles        = domain.BuiltInRoles()
	enforcerMu          sync.RWMutex
	enforcer            *casbin.SyncedEnforcer
	resolveSubjectRoles = func(userID, systemRole int) []string {
		return domain.ResolveSystemRoles(systemRole, common.RoleRootUser, common.RoleAdminUser)
	}
)

func UserSubject(id int) string                    { return domain.UserSubject(id) }
func RoleSubject(role string) string               { return domain.RoleSubject(role) }
func RegisterResource(resource ResourceDefinition) { registry = append(registry, resource) }
func currentCatalog() domain.Catalog {
	return domain.Catalog{Resources: registry, BuiltInRoles: builtInRoles}
}
func Catalog() []ResourceDefinition {
	return currentCatalog().ResourcesCopy()
}
func Roles() []RoleDescriptor                     { return currentCatalog().Roles() }
func AllPermissions() []Permission                { return currentCatalog().AllPermissions() }
func PermissionsForRole(role string) []Permission { return currentCatalog().PermissionsForRole(role) }
func authorizationState() casbinadapter.State     { return casbinadapter.NewState(&enforcerMu, &enforcer) }
func currentEnforcer() *casbin.SyncedEnforcer     { return authorizationState().Current() }
func readPolicy() domain.PolicyReader {
	p := casbinadapter.NewPolicy(currentEnforcer())
	if p == nil {
		return nil
	}
	return p
}
func writePolicy() set_user_permissions.Policy {
	p := casbinadapter.NewPolicy(currentEnforcer())
	if p == nil {
		return nil
	}
	return p
}
func accessRules() domain.AccessRules {
	return domain.AccessRules{Catalog: currentCatalog, ResolveRoles: func(id, role int) []string { return resolveSubjectRoles(id, role) }, Policy: readPolicy}
}
func Can(id, role int, permission Permission) bool {
	return check_permission.NewUsecase(accessRules()).Execute(id, role, permission)
}
func Capabilities(id, role int) PermissionsMap {
	return get_capabilities.NewUsecase(currentCatalog, accessRules()).Execute(id, role)
}
func ExplicitUserPermissions(id int) PermissionsMap {
	return get_capabilities.NewUsecase(currentCatalog, accessRules()).Execute(id, common.RoleAdminUser)
}
func ExplicitUserOverrides(id int) PermissionsMap {
	return get_user_overrides.NewUsecase(currentCatalog, readPolicy).Execute(id)
}
func SetUserPermissions(id int, permissions PermissionsMap) error {
	return set_user_permissions.NewUsecase(currentCatalog, writePolicy).Execute(id, permissions)
}
func SetUserPermissionsInTx(tx *gorm.DB, id int, permissions PermissionsMap) error {
	return set_user_permissions_in_tx.NewUsecase(currentCatalog, readPolicy, repository.NewStore(tx)).Execute(id, permissions)
}
func ClearUserPermissions(id int) error {
	return clear_user_permissions.NewUsecase(currentCatalog, func() clear_user_permissions.Writer {
		p := casbinadapter.NewPolicy(currentEnforcer())
		if p == nil {
			return nil
		}
		return p
	}).Execute(id)
}
func ClearUserPermissionsInTx(tx *gorm.DB, id int) error {
	return clear_user_permissions_in_tx.NewUsecase(currentCatalog, repository.NewStore(tx)).Execute(id)
}
func ClearUserAuthorization(id int) error                  { return ClearUserPermissions(id) }
func ClearUserAuthorizationInTx(tx *gorm.DB, id int) error { return ClearUserPermissionsInTx(tx, id) }
func Init(db *gorm.DB) error {
	return initialize_authorization.NewUsecase(currentCatalog, func() bool { return common.IsMasterNode }, repository.NewStore(db), casbinadapter.NewEngine(authorizationState(), repository.NewPolicyAdapter(db))).Execute()
}
func ReloadPolicy() error { return reload_policy.NewUsecase(authorizationState().Reload).Execute() }
func StartPolicySync(frequency int) {
	sync_policy.NewUsecase(authorizationState().Reload, time.Sleep, common.SysError).Execute(frequency)
}
