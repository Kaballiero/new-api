package model

import (
	"errors"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// External DSNs must point to an isolated test database: this fixture clears options.
func useGroupRatioOptionDB(t *testing.T) *gorm.DB {
	t.Helper()
	var dialect gorm.Dialector = sqlite.Open(":memory:")
	if dsn := os.Getenv("GROUP_RATIO_TEST_MYSQL_DSN"); dsn != "" {
		dialect = mysql.Open(dsn)
	} else if dsn := os.Getenv("GROUP_RATIO_TEST_POSTGRES_DSN"); dsn != "" {
		dialect = postgres.Open(dsn)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Where("1 = 1").Delete(&Option{}).Error)
	previousDB, previousOptions := DB, common.OptionMap
	previousRatio := ratio_setting.GroupRatio2JSONString()
	previousSpecial := ratio_setting.GroupGroupRatio2JSONString()
	DB, common.OptionMap = db, map[string]string{}
	t.Cleanup(func() {
		DB, common.OptionMap = previousDB, previousOptions
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatio))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(previousSpecial))
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestGroupRatioOptionsReload(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options []Option
		want    string
	}{
		{"legacy-first", []Option{{"GroupRatio", `{"default":1.45}`}, {"group_ratio_setting.group_ratio", `{"default":1}`}}, `{"default":1.45}`},
		{"alias-first", []Option{{"group_ratio_setting.group_ratio", `{"default":1}`}, {"GroupRatio", `{"default":1.45}`}}, `{"default":1.45}`},
		{"alias-only", []Option{{"group_ratio_setting.group_ratio", `{"default":1.45}`}}, `{"default":1.45}`},
		{"legacy-only-free-group", []Option{{"GroupRatio", `{"default":0}`}}, `{"default":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := useGroupRatioOptionDB(t)
			require.NoError(t, db.Create(&tc.options).Error)
			for range 2 {
				loadOptionsFromDatabase()
				assert.JSONEq(t, tc.want, ratio_setting.GroupRatio2JSONString())
				assert.JSONEq(t, tc.want, common.OptionMap["GroupRatio"])
				assert.JSONEq(t, tc.want, common.OptionMap["group_ratio_setting.group_ratio"])
			}
		})
	}
}

func TestGroupRatioOptionsWriteAliases(t *testing.T) {
	for _, tc := range []struct {
		canonical, alias, value string
	}{
		{"GroupRatio", "group_ratio_setting.group_ratio", `{"default":1.45}`},
		{"GroupGroupRatio", "group_ratio_setting.group_group_ratio", `{"default":{"default":1.2}}`},
	} {
		for _, key := range []string{tc.canonical, tc.alias} {
			t.Run(key, func(t *testing.T) {
				db := useGroupRatioOptionDB(t)
				require.NoError(t, db.Create(&[]Option{{tc.canonical, `{}`}, {tc.alias, `{}`}}).Error)
				require.NoError(t, UpdateOption(key, tc.value))
				for _, spelling := range []string{tc.canonical, tc.alias} {
					assert.JSONEq(t, tc.value, requireOptionValue(t, db, spelling))
					assert.JSONEq(t, tc.value, common.OptionMap[spelling])
				}
				loadOptionsFromDatabase()
				if tc.canonical == "GroupRatio" {
					assert.Equal(t, 1.45, ratio_setting.GetGroupRatio("default"))
					// Incident 358225: 274 input + 1 output, model ratio .05,
					// completion ratio 4, captured USD/RUB 84.3508.
					assert.Equal(t, 17, common.QuotaRound((274+4)*0.05*(84.3508/100)*ratio_setting.GetGroupRatio("default")))
				} else {
					ratio, ok := ratio_setting.GetGroupGroupRatio("default", "default")
					require.True(t, ok)
					assert.Equal(t, 1.2, ratio)
				}
			})
		}
	}
}

func TestGroupRatioOptionsRejectInvalidWrites(t *testing.T) {
	db := useGroupRatioOptionDB(t)
	require.NoError(t, UpdateOption("GroupRatio", `{"default":1.45}`))
	for _, value := range []string{`{"default":-1}`, `{"default":`, `null`, `{"default":1e999}`} {
		for _, key := range []string{"GroupRatio", "group_ratio_setting.group_ratio"} {
			require.Error(t, UpdateOption(key, value))
			require.Error(t, updateOptionMap(key, value))
			assert.Equal(t, 1.45, ratio_setting.GetGroupRatio("default"))
			assert.JSONEq(t, `{"default":1.45}`, common.OptionMap[key])
			assert.JSONEq(t, `{"default":1.45}`, requireOptionValue(t, db, key))
		}
	}
	require.NoError(t, UpdateOption("GroupGroupRatio", `{"default":{"default":1.2}}`))
	for _, value := range []string{`{"default":{"default":-1}}`, `{"default":`, `null`} {
		require.Error(t, UpdateOption("group_ratio_setting.group_group_ratio", value))
		ratio, ok := ratio_setting.GetGroupGroupRatio("default", "default")
		require.True(t, ok)
		assert.Equal(t, 1.2, ratio)
	}
	require.Error(t, UpdateOptionsBulk(map[string]string{
		"GroupRatio":                      `{"default":2}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))
	assert.Equal(t, 1.45, ratio_setting.GetGroupRatio("default"))
}

func TestGroupRatioOptionsRollback(t *testing.T) {
	db := useGroupRatioOptionDB(t)
	require.NoError(t, UpdateOption("GroupRatio", `{"default":1.45}`))
	updates := 0
	errWrite := errors.New("injected second option write failure")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:fail_ratio_alias", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			updates++
			if updates == 2 {
				tx.AddError(errWrite)
			}
		}
	}))
	require.ErrorIs(t, UpdateOption("group_ratio_setting.group_ratio", `{"default":2}`), errWrite)
	require.NoError(t, db.Callback().Update().Remove("test:fail_ratio_alias"))
	assert.Equal(t, 2, updates)
	for _, key := range []string{"GroupRatio", "group_ratio_setting.group_ratio"} {
		assert.JSONEq(t, `{"default":1.45}`, requireOptionValue(t, db, key))
		assert.JSONEq(t, `{"default":1.45}`, common.OptionMap[key])
	}
	assert.Equal(t, 1.45, ratio_setting.GetGroupRatio("default"))
}

func TestGroupGroupRatioOptionsReload(t *testing.T) {
	db := useGroupRatioOptionDB(t)
	require.NoError(t, db.Create(&[]Option{
		{"GroupRatio", `{"default":1.45}`},
		{"GroupGroupRatio", `{"default":{"default":1.1}}`},
		{"group_ratio_setting.group_group_ratio", `{"default":{"default":1}}`},
	}).Error)
	for range 2 {
		loadOptionsFromDatabase()
		ratio, ok := ratio_setting.GetGroupGroupRatio("default", "default")
		require.True(t, ok)
		assert.Equal(t, 1.1, ratio, "explicit user-group override remains independent of base ratio")
		assert.Equal(t, 1.45, ratio_setting.GetGroupRatio("default"))
		assert.JSONEq(t, `{"default":{"default":1.1}}`, common.OptionMap["GroupGroupRatio"])
		assert.JSONEq(t, common.OptionMap["GroupGroupRatio"], common.OptionMap["group_ratio_setting.group_group_ratio"])
	}
}
