package authz

import (
	"errors"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/authorization"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
	return openAuthzTestDB(t)
}

// External runners supply a fresh, isolated test database for each test.
func openAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	var dialect gorm.Dialector
	switch engine := os.Getenv("TEST_AUTHORIZATION_DIALECT"); engine {
	case "", "sqlite":
		dsn := os.Getenv("TEST_AUTHORIZATION_UPGRADE_DSN")
		if dsn == "" {
			dsn = ":memory:"
		}
		dialect = sqlite.Open(dsn)
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		require.NotEmpty(t, dsn)
		dialect = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		require.NotEmpty(t, dsn)
		dialect = postgres.Open(dsn)
	default:
		t.Fatalf("unsupported authorization test dialect %q", engine)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	return db
}

func TestInitSeedsBuiltInRolesAndPoliciesOnce(t *testing.T) {
	db := newAuthzTestDB(t)

	require.NoError(t, Init(db))
	require.NoError(t, Init(db))

	// root is a superuser role and is granted everything implicitly, so only the
	// admin baseline is written as explicit policy rows.
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&count).Error)
	assert.Equal(t, int64(len(PermissionsForRole(BuiltInRoleAdmin))), count)

	var roles []model.AuthzRole
	require.NoError(t, db.Order("sort asc").Find(&roles).Error)
	require.Len(t, roles, 2)
	assert.Equal(t, BuiltInRoleRoot, roles[0].Key)
	assert.Equal(t, BuiltInRoleAdmin, roles[1].Key)

	assert.True(t, Can(1, common.RoleRootUser, ChannelSensitiveWrite))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelOperate))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(2, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(3, common.RoleCommonUser, ChannelRead))
}

func TestInitOnSlaveOnlyLoadsPolicies(t *testing.T) {
	wasMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
	db := openAuthzTestDB(t)

	require.NoError(t, Init(db))

	var roleCount int64
	require.NoError(t, db.Model(&model.AuthzRole{}).Count(&roleCount).Error)
	assert.Equal(t, int64(0), roleCount)
	var policyCount int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&policyCount).Error)
	assert.Equal(t, int64(0), policyCount)
	assert.False(t, Can(2, common.RoleAdminUser, ChannelRead))
}

func TestSetUserPermissionsStoresOnlyOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(42, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          false,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
			"unknown":            true,
		},
		"unknown": {
			ActionRead: true,
		},
	}))

	assert.True(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(42, common.RoleAdminUser, ChannelWrite))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          false,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
		},
		ResourceTaskPlugin: {
			ActionBind: false,
		},
		ResourceAudit: {ActionRead: false},
	}, ExplicitUserPermissions(42))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionSensitiveWrite: true,
			ActionWrite:          false,
		},
	}, ExplicitUserOverrides(42))

	var userPolicyCount int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(42)).Count(&userPolicyCount).Error)
	assert.Equal(t, int64(2), userPolicyCount)

	require.NoError(t, SetUserPermissions(42, PermissionsMap{ResourceChannel: {
		ActionRead:           true,
		ActionOperate:        true,
		ActionWrite:          true,
		ActionSensitiveWrite: false,
		ActionSecretView:     false,
	}}))
	assert.False(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          true,
			ActionSensitiveWrite: false,
			ActionSecretView:     false,
		},
		ResourceTaskPlugin: {
			ActionBind: false,
		},
		ResourceAudit: {ActionRead: false},
	}, ExplicitUserPermissions(42))
	assert.Empty(t, ExplicitUserOverrides(42))
}

func TestClearUserAuthorizationRemovesOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(90, PermissionsMap{ResourceChannel: {
		ActionWrite:          false,
		ActionSensitiveWrite: true,
	}}))

	assert.True(t, Can(90, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(90, common.RoleAdminUser, ChannelWrite))

	require.NoError(t, ClearUserAuthorization(90))

	assert.Empty(t, ExplicitUserOverrides(90))
	assert.True(t, Can(90, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(90, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(90, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(90, common.RoleCommonUser, ChannelRead))
}

func TestSetUserPermissionsInTxDoesNotMutateEnforcerBeforeReload(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetUserPermissionsInTx(tx, 42, PermissionsMap{ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          true,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
		}})
	}))

	assert.False(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	require.NoError(t, ReloadPolicy())
	assert.True(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
}

func TestSetUserPermissionsInTxRollbackLeavesNoPolicy(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, SetUserPermissionsInTx(tx, 43, PermissionsMap{ResourceChannel: {
		ActionSensitiveWrite: true,
	}}))
	require.NoError(t, tx.Rollback().Error)
	require.NoError(t, ReloadPolicy())

	assert.False(t, Can(43, common.RoleAdminUser, ChannelSensitiveWrite))
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(43)).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestAdapterAddPolicyIsIdempotent(t *testing.T) {
	db := newAuthzTestDB(t)
	adapter := repository.NewPolicyAdapter(db)
	rule := []string{UserSubject(55), ResourceChannel, ActionSensitiveWrite, EffectAllow}

	require.NoError(t, adapter.AddPolicy("p", "p", rule))
	require.NoError(t, adapter.AddPolicy("p", "p", rule))

	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where(
		"ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ?",
		"p",
		UserSubject(55),
		ResourceChannel,
		ActionSensitiveWrite,
		EffectAllow,
	).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCapabilitiesUseCatalogShape(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	capabilities := Capabilities(7, common.RoleAdminUser)

	assert.True(t, capabilities[ResourceChannel][ActionRead])
	assert.True(t, capabilities[ResourceChannel][ActionOperate])
	assert.True(t, capabilities[ResourceChannel][ActionWrite])
	assert.False(t, capabilities[ResourceChannel][ActionSensitiveWrite])
	assert.False(t, capabilities[ResourceChannel][ActionSecretView])
	assert.False(t, capabilities[ResourceTaskPlugin][ActionBind])
}

func TestTaskPluginBindIsRootOnlyUntilGranted(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	var bindAction *ActionDefinition
	for _, resource := range Catalog() {
		if resource.Resource != ResourceTaskPlugin {
			continue
		}
		assert.Equal(t, "Task Plugin", resource.LabelKey)
		for i := range resource.Actions {
			if resource.Actions[i].Action == ActionBind {
				bindAction = &resource.Actions[i]
			}
		}
	}
	require.NotNil(t, bindAction)
	assert.Equal(t, "Bind task plugins", bindAction.LabelKey)
	assert.Equal(t, "List registered task plugins and bind them when creating or editing task plugin channels.", bindAction.DescriptionKey)
	assert.Empty(t, bindAction.DefaultRoles)

	assert.False(t, Can(2, common.RoleAdminUser, TaskPluginBind))
	assert.True(t, Can(1, common.RoleRootUser, TaskPluginBind))

	enforcer := currentEnforcer()
	require.NotNil(t, enforcer)
	_, err := enforcer.AddPolicy(RoleSubject(BuiltInRoleAdmin), ResourceTaskPlugin, ActionBind, EffectAllow)
	require.NoError(t, err)
	assert.True(t, Can(2, common.RoleAdminUser, TaskPluginBind))

	_, err = enforcer.RemovePolicy(RoleSubject(BuiltInRoleAdmin), ResourceTaskPlugin, ActionBind, EffectAllow)
	require.NoError(t, err)
	assert.False(t, Can(2, common.RoleAdminUser, TaskPluginBind))
}

func TestAuthorizationFailureAndOverrideContracts(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	unknown := Permission{Resource: "unregistered", Action: "unknown"}
	assert.True(t, Can(7, common.RoleRootUser, unknown))
	assert.False(t, Can(7, common.RoleAdminUser, unknown))
	e := currentEnforcer()
	_, err := e.AddPolicy(UserSubject(7), ChannelRead.Resource, ChannelRead.Action, EffectAllow)
	require.NoError(t, err)
	_, err = e.AddPolicy(UserSubject(7), ChannelRead.Resource, ChannelRead.Action, EffectDeny)
	require.NoError(t, err)
	assert.False(t, Can(7, common.RoleAdminUser, ChannelRead), "an explicit deny wins even when an allow also exists")
	enforcerMu.Lock()
	enforcer = nil
	enforcerMu.Unlock()
	t.Cleanup(func() { enforcerMu.Lock(); enforcer = e; enforcerMu.Unlock() })
	assert.False(t, Can(7, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(7, common.RoleRootUser, unknown))
	require.Error(t, SetUserPermissions(7, PermissionsMap{}))
	require.Error(t, ClearUserPermissions(7))
	sentinel := errors.New("rollback cleanup")
	err = db.Transaction(func(tx *gorm.DB) error {
		require.Error(t, SetUserPermissionsInTx(tx, 7, PermissionsMap{}))
		require.NoError(t, ClearUserPermissionsInTx(tx, 7))
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(7)).Count(&count).Error)
	assert.EqualValues(t, 2, count, "cleanup must participate in the caller's transaction")
}

// The external matrix runs baseline and candidate on the same persisted database.
func TestAuthorizationStorageUpgradeCompatibility(t *testing.T) {
	phase := os.Getenv("TEST_AUTHORIZATION_UPGRADE_PHASE")
	if phase == "" {
		t.Skip("TEST_AUTHORIZATION_UPGRADE_PHASE is not configured")
	}
	require.Contains(t, []string{"baseline", "candidate"}, phase)
	db := newAuthzTestDB(t)
	if phase == "baseline" {
		require.NoError(t, Init(db))
		require.NoError(t, db.Create(&model.AuthzRole{Key: "persisted-custom-role", Name: "Custom", Enabled: false, Sort: 55}).Error)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return SetUserPermissionsInTx(tx, 4242, PermissionsMap{
				ResourceChannel: {ActionRead: false, ActionSensitiveWrite: true},
				ResourceAudit:   {ActionRead: true},
			})
		}))
	}
	var custom model.AuthzRole
	require.NoError(t, db.Where(&model.AuthzRole{Key: "persisted-custom-role"}).First(&custom).Error)
	assert.Equal(t, "Custom", custom.Name)
	assert.False(t, custom.Enabled)
	assert.Equal(t, 55, custom.Sort)
	var overrides int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(4242)).Count(&overrides).Error)
	assert.EqualValues(t, 3, overrides)
	for range 2 {
		require.NoError(t, Init(db))
	}
	assert.False(t, Can(4242, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(4242, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.True(t, Can(4242, common.RoleAdminUser, AuditRead))
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(4242)).Count(&overrides).Error)
	assert.EqualValues(t, 3, overrides)
	require.Error(t, db.Create(&model.AuthzRole{Key: BuiltInRoleRoot, Name: "duplicate"}).Error)
	require.Error(t, db.Create(&model.CasbinRule{Ptype: "p", V0: UserSubject(4242), V1: ResourceChannel, V2: ActionRead, V3: EffectDeny}).Error)
}
