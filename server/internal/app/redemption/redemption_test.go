package redemption

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/redemption"
	"github.com/QuantumNous/new-api/internal/adapter/wallet"
	"github.com/QuantumNous/new-api/internal/redemption/create_codes"
	"github.com/QuantumNous/new-api/internal/redemption/delete_codes"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
	"github.com/QuantumNous/new-api/internal/redemption/list_codes"
	"github.com/QuantumNous/new-api/internal/redemption/redeem_code"
	"github.com/QuantumNous/new-api/internal/redemption/search_codes"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func isolatedDatabase(t *testing.T, kind string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch kind {
	case "sqlite":
		driver = sqlite.Open(t.TempDir() + "/redemption.db?_pragma=busy_timeout(30000)&_txlock=immediate")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		cfg, err := pgx.ParseConfig(dsn)
		require.NoError(t, err)
		adminSQL := stdlib.OpenDB(*cfg)
		admin, err := gorm.Open(postgres.New(postgres.Config{Conn: adminSQL}), &gorm.Config{})
		require.NoError(t, err)
		schema := "redemption_test_" + common.GetUUID()
		quoted := pgx.Identifier{schema}.Sanitize()
		require.NoError(t, admin.Exec("CREATE SCHEMA "+quoted).Error)
		cfg.RuntimeParams["search_path"] = schema
		sqlDB := stdlib.OpenDB(*cfg)
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
		require.NoError(t, err)
		t.Cleanup(func() {
			assert.NoError(t, sqlDB.Close())
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			assert.NoError(t, admin.WithContext(ctx).Exec("DROP SCHEMA "+quoted+" CASCADE").Error)
			assert.NoError(t, adminSQL.Close())
		})
		connections := []interface{ Close() error }{}
		defer func() {
			for _, connection := range connections {
				assert.NoError(t, connection.Close())
			}
		}()
		for range 2 {
			connection, err := sqlDB.Conn(t.Context())
			require.NoError(t, err)
			connections = append(connections, connection)
			var current string
			require.NoError(t, connection.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&current))
			require.Equal(t, schema, current)
		}
		t.Logf("isolated PostgreSQL schema: %s", schema)
		return db
	default:
		t.Fatalf("unsupported database %s", kind)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	return db
}

func newPersistence(t *testing.T, kind string) (*gorm.DB, *gorm.DB) {
	t.Helper()
	require.NoError(t, i18n.Init())
	db, logDB := isolatedDatabase(t, kind), isolatedDatabase(t, kind)
	previousDB, previousLog := model.DB, model.LOG_DB
	previousMain, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, model.LOG_DB = db, logDB
	databaseType := common.DatabaseTypeSQLite
	if kind == "postgres" {
		databaseType = common.DatabaseTypePostgreSQL
	}
	if kind == "mysql" {
		databaseType = common.DatabaseTypeMySQL
	}
	common.SetDatabaseTypes(databaseType, databaseType)
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLog
		common.SetDatabaseTypes(previousMain, previousLogKind)
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
	for _, table := range []any{&model.User{}, &model.Redemption{}} {
		require.False(t, db.Migrator().HasTable(table), "use an isolated empty test schema")
		require.NoError(t, db.AutoMigrate(table))
		t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(table)) })
	}
	for _, table := range []any{&model.AuditLog{}, &model.Log{}} {
		require.False(t, logDB.Migrator().HasTable(table), "use an isolated empty log schema")
		require.NoError(t, logDB.AutoMigrate(table))
		t.Cleanup(func() { assert.NoError(t, logDB.Migrator().DropTable(table)) })
	}
	var version string
	query := "SELECT version()"
	if kind == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("database version: %s", version)
	return db, logDB
}

func TestDeleteRedemptionBatch(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db, logDB := newPersistence(t, dialect)
			token := "redemption-audit-test-token"
			admin := model.User{Username: "redemption-audit-admin", Password: "unused", Role: common.RoleAdminUser, Status: common.UserStatusEnabled,
				Group: "default", AccessToken: &token}
			require.NoError(t, db.Create(&admin).Error)
			codes := make([]model.Redemption, 16)
			for index := range codes {
				codes[index] = model.Redemption{Name: "selected", Key: fmt.Sprintf("%032d", index+1), Quota: 100, Status: domain.StatusEnabled}
			}
			codes[1].Status = domain.StatusUsed
			codes[15].Name = "unselected"
			codes[15].Status = domain.StatusDisabled
			require.NoError(t, model.DB.Create(&codes).Error)
			router := gin.New()
			router.Use(middleware.RequestId())
			router.POST("/api/redemption/batch", middleware.AdminAuth(), NewHTTP().DeleteBatch.Handle)

			overLimit := make([]int, 1001)
			for index := range overLimit {
				overLimit[index] = codes[0].Id
			}
			oversized, err := common.Marshal(map[string]any{"ids": overLimit})
			require.NoError(t, err)
			for _, body := range []string{"{}", `{"ids":[]}`, `{"ids":null}`, `{"ids":[0]}`, `{"ids":[1,-1]}`, `{"ids":["1"]}`, "{", string(oversized)} {
				t.Run("invalid_"+body[:min(len(body), 30)], func(t *testing.T) {
					response := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, "/api/redemption/batch", bytes.NewBufferString(body))
					request.Header.Set("Authorization", "Bearer "+token)
					router.ServeHTTP(response, request)
					var result struct {
						Success bool `json:"success"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.False(t, result.Success)
					var count int64
					require.NoError(t, model.DB.Model(&model.Redemption{}).Count(&count).Error)
					assert.EqualValues(t, 16, count)
					var events []model.AuditLog
					require.NoError(t, logDB.Where("request_id = ? AND category = ?", response.Header().Get(common.RequestIdKey),
						model.AuditCategoryOperation).Find(&events).Error)
					require.Len(t, events, 1)
					assert.False(t, events[0].Success)
					assert.Equal(t, "redemption.delete_batch", events[0].Action)
				})
			}
			_, err = delete_codes.NewUsecase(storage.NewStore(func() *gorm.DB { return model.DB }, model.LockForUpdate, wallet.Credit)).Execute(nil)
			require.Error(t, err)
			requestedIDs := make([]int, 0, 17)
			for _, code := range codes[:15] {
				requestedIDs = append(requestedIDs, code.Id)
			}
			requestedIDs = append(requestedIDs, codes[0].Id, 999999)
			payload, err := common.Marshal(map[string]any{"ids": requestedIDs})
			require.NoError(t, err)
			for _, expectedCount := range []int64{15, 0} {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/redemption/batch", bytes.NewReader(payload))
				request.Header.Set("Authorization", "Bearer "+token)
				router.ServeHTTP(response, request)
				assert.Equal(t, http.StatusOK, response.Code)
				var result struct {
					Success bool  `json:"success"`
					Data    int64 `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				assert.True(t, result.Success)
				assert.Equal(t, expectedCount, result.Data)
				var events []model.AuditLog
				require.NoError(t, logDB.Where("request_id = ? AND category = ?", response.Header().Get(common.RequestIdKey),
					model.AuditCategoryOperation).Find(&events).Error)
				require.Len(t, events, 1, "one operation event, without a duplicate single-delete fallback")
				event := events[0]
				assert.Equal(t, "redemption.delete_batch", event.Action)
				assert.Equal(t, fmt.Sprintf("Batch deleted %d redemption codes", expectedCount), event.Content)
				assert.True(t, event.Success)
				assert.Equal(t, admin.Id, event.UserId)
				assert.Equal(t, "/api/redemption/batch", event.Route)
				require.NotNil(t, event.Other.Op)
				encoded, err := common.Marshal(event.Other.Op.Params)
				require.NoError(t, err)
				var params struct {
					Count int64 `json:"count"`
					Total int   `json:"total"`
					IDs   []int `json:"requested_redemption_ids"`
				}
				require.NoError(t, common.Unmarshal(encoded, &params))
				assert.Equal(t, expectedCount, params.Count)
				assert.Equal(t, len(requestedIDs), params.Total)
				assert.Equal(t, requestedIDs, params.IDs)
				encoded, err = common.Marshal(event)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), token)
				for _, code := range codes {
					assert.NotContains(t, string(encoded), code.Key)
				}
			}
			var active []model.Redemption
			require.NoError(t, model.DB.Find(&active).Error)
			require.Len(t, active, 1)
			assert.Equal(t, codes[15], active[0])
			var all []model.Redemption
			require.NoError(t, model.DB.Unscoped().Order("id").Find(&all).Error)
			require.Len(t, all, 16)
			for _, code := range all[:15] {
				assert.True(t, code.DeletedAt.Valid)
			}
			assert.False(t, all[15].DeletedAt.Valid)
		})
	}
}

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			newPersistence(t, dialect)
			search := search_codes.NewUsecase(storage.NewStore(func() *gorm.DB { return model.DB }, model.LockForUpdate, wallet.Credit),
				common.GetTimestamp)

			require.NoError(t, model.DB.AutoMigrate(&model.Redemption{}))
			require.NoError(t, model.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Redemption{}).Error)
			t.Cleanup(func() {
				require.NoError(t, model.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Redemption{}).Error)
			})

			now := common.GetTimestamp()
			redemptions := []model.Redemption{
				{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: domain.StatusEnabled, ExpiredTime: 0},
				{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: domain.StatusEnabled, ExpiredTime: now + 3600},
				{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: domain.StatusEnabled, ExpiredTime: now - 10},
				{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: domain.StatusDisabled, ExpiredTime: 0},
				{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: domain.StatusUsed, ExpiredTime: 0},
			}
			require.NoError(t, model.DB.Create(&redemptions).Error)

			tests := []struct {
				name      string
				keyword   string
				status    string
				startIdx  int
				num       int
				wantTotal int64
				wantIds   []int
			}{
				{
					name:      "no filters returns all rows",
					num:       10,
					wantTotal: 5,
					wantIds:   []int{5, 4, 3, 2, 1},
				},
				{
					name:      "keyword filters by name prefix",
					keyword:   "alpha",
					num:       10,
					wantTotal: 3,
					wantIds:   []int{3, 2, 1},
				},
				{
					name:      "enabled status excludes expired rows",
					status:    "1",
					num:       10,
					wantTotal: 2,
					wantIds:   []int{2, 1},
				},
				{
					name:      "expired status returns enabled expired rows",
					status:    "expired",
					num:       10,
					wantTotal: 1,
					wantIds:   []int{3},
				},
				{
					name:      "disabled status",
					status:    "2",
					num:       10,
					wantTotal: 1,
					wantIds:   []int{4},
				},
				{
					name:      "used status",
					status:    "3",
					num:       10,
					wantTotal: 1,
					wantIds:   []int{5},
				},
				{
					name:      "pagination keeps unpaged total",
					startIdx:  1,
					num:       2,
					wantTotal: 5,
					wantIds:   []int{4, 3},
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					result, err := search.Execute(search_codes.Input{Keyword: tt.keyword, Status: tt.status, Start: tt.startIdx, Limit: tt.num})
					rows, total := result.Codes, result.Total
					require.NoError(t, err)
					assert.Equal(t, tt.wantTotal, total)
					gotIds := make([]int, 0, len(rows))
					for _, row := range rows {
						gotIds = append(gotIds, row.ID)
					}
					assert.Equal(t, tt.wantIds, gotIds)
				})
			}

		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.Redemption{}))
	require.NoError(t, model.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.Redemption{}).Error)

	user := &model.User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, model.DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &model.Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      domain.StatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, model.DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			newPersistence(t, dialect)
			userId, key := setupRedeemFixture(t, 500)

			quota, err := Redeemer().Execute(redeem_code.Input{Key: key, UserID: userId})
			require.NoError(t, err)
			assert.Equal(t, 500, quota)

			var user model.User
			require.NoError(t, model.DB.First(&user, "id = ?", userId).Error)
			assert.Equal(t, 500, user.Quota)

			var redemption model.Redemption
			require.NoError(t, model.DB.First(&redemption, "name = ?", "redeem-test").Error)
			assert.Equal(t, domain.StatusUsed, redemption.Status)
			assert.Equal(t, userId, redemption.UsedUserId)

			// Redeeming the same code again must fail and must not credit quota.
			_, err = Redeemer().Execute(redeem_code.Input{Key: key, UserID: userId})
			require.Error(t, err)
			require.NoError(t, model.DB.First(&user, "id = ?", userId).Error)
			assert.Equal(t, 500, user.Quota)

		})
	}
}

func TestRedeemRejectsWalletOverflow(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			newPersistence(t, dialect)
			userId, key := setupRedeemFixture(t, 11)
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userId).Update("quota", common.MaxWalletQuota-10).Error)

			_, err := Redeemer().Execute(redeem_code.Input{Key: key, UserID: userId})
			require.ErrorIs(t, err, domain.ErrRedeemFailed)

			var user model.User
			require.NoError(t, model.DB.First(&user, "id = ?", userId).Error)
			assert.Equal(t, common.MaxWalletQuota-10, user.Quota)

			var redemption model.Redemption
			require.NoError(t, model.DB.First(&redemption, "key = ?", key).Error)
			assert.Equal(t, domain.StatusEnabled, redemption.Status)

		})
	}
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			newPersistence(t, dialect)
			setupRedeemFixture(t, 500)

			redemption := &model.Redemption{
				Name:        "overflow-redemption",
				Key:         "10000000000000000000000000000002",
				Status:      domain.StatusEnabled,
				Quota:       common.MaxWalletQuota + 1,
				CreatedTime: common.GetTimestamp(),
			}
			_, err := create_codes.NewUsecase(storage.NewStore(func() *gorm.DB { return model.DB }, model.LockForUpdate, wallet.Credit),
				func(quota int) error { return common.ValidateWalletQuota(quota) }, common.GetTimestamp, common.GetUUID).
				Execute(create_codes.Input{Name: redemption.Name, Count: 1, Quota: redemption.Quota})
			require.Error(t, err)
		})
	}
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.

func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			newPersistence(t, dialect)
			userId, key := setupRedeemFixture(t, 300)

			const goroutines = 5
			successes := make([]bool, goroutines)
			var wg sync.WaitGroup
			wg.Add(goroutines)
			for i := range goroutines {
				go func(idx int) {
					defer wg.Done()
					if _, err := Redeemer().Execute(redeem_code.Input{Key: key, UserID: userId}); err == nil {
						successes[idx] = true
					}
				}(i)
			}
			wg.Wait()

			successCount := 0
			for _, ok := range successes {
				if ok {
					successCount++
				}
			}
			assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

			var user model.User
			require.NoError(t, model.DB.First(&user, "id = ?", userId).Error)
			assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")

		})
	}
}

func TestFailureLogAcceptsMissingGinRequest(t *testing.T) {
	for _, ctx := range []*gin.Context{nil, {}, {Request: httptest.NewRequest("POST", "/api/user/topup", nil)}} {
		require.NotPanics(t, func() { logRedemptionFailure(ctx, "test-code", 7, domain.ErrRedeemFailed) })
	}
}

func TestReadRecoveryPreservesScannedDestinations(t *testing.T) {
	for _, stage := range []string{"count", "find"} {
		t.Run(stage, func(t *testing.T) {
			db, _ := newPersistence(t, "sqlite")
			require.NoError(t, db.Create(&[]model.Redemption{
				{Id: 1, Key: "one", Name: "one", Quota: 100}, {Id: 2, Key: "two", Name: "two", Quota: 100},
			}).Error)
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("redemption_read_panic", func(tx *gorm.DB) {
				if tx.Statement.Table != "redemptions" {
					return
				}
				_, count := tx.Statement.Dest.(*int64)
				if (stage == "count") == count {
					panic("after scan")
				}
			}))
			t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove("redemption_read_panic")) })
			store := storage.NewStore(func() *gorm.DB { return model.DB }, model.LockForUpdate, wallet.Credit)
			result, err := list_codes.NewUsecase(store).Execute(list_codes.Input{Limit: 10})
			require.NoError(t, err)
			assert.EqualValues(t, 2, result.Total)
			if stage == "count" {
				assert.Nil(t, result.Codes)
			} else {
				require.Len(t, result.Codes, 2)
				assert.Equal(t, 2, result.Codes[0].ID)
				assert.Equal(t, 1, result.Codes[1].ID)
			}
		})
	}
}

func TestReceiptLanguageAliases(t *testing.T) {
	db, _ := newPersistence(t, "sqlite")
	user := model.User{Id: 91, Username: "receipt-language", Password: "unused"}
	require.NoError(t, db.Create(&user).Error)
	for _, tc := range []struct{ language, want string }{
		{"", i18n.LangEn}, {"en-US", i18n.LangEn}, {"unknown", i18n.LangEn},
		{"ru", i18n.LangRu}, {"ru-RU", i18n.LangRu}, {"RU", i18n.LangRu},
		{"zh", i18n.LangZhCN}, {"zhCN", i18n.LangZhCN}, {"zh-CN", i18n.LangZhCN}, {"zh-TW", i18n.LangZhCN},
	} {
		t.Run(tc.language, func(t *testing.T) {
			setting, err := common.Marshal(map[string]any{"language": tc.language})
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("setting", string(setting)).Error)
			assert.Equal(t, tc.want, receiptLanguage(user.Id))
		})
	}
}

func TestQuotaTextPreservesCustomDollar(t *testing.T) {
	require.NoError(t, i18n.Init())
	settings := operation_setting.GetGeneralSetting()
	previous := *settings
	previousQuota := common.QuotaPerUnit
	t.Cleanup(func() { *settings = previous; common.QuotaPerUnit = previousQuota })
	common.QuotaPerUnit = 500 * 1000.0
	settings.QuotaDisplayType = operation_setting.QuotaDisplayTypeCustom
	settings.CustomCurrencySymbol = "\uff04"
	settings.CustomCurrencyExchangeRate = 1
	assert.Equal(t, "\uff040.001000 quota", quotaText(500, i18n.LangEn))
	assert.Equal(t, "\uff040.001000 единиц квоты", quotaText(500, i18n.LangRu))
}

func TestMaskCodeHidesTheCode(t *testing.T) {
	assert.Equal(t, "0123***cdef", maskCode("0123456789abcdef0123456789abcdef"))
	assert.Equal(t, "***", maskCode("short-code"))
	assert.Equal(t, "***", maskCode(""))
}
