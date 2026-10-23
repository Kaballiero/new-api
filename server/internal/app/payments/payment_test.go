package payments

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/payments"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func isolatedDatabase(t *testing.T, kind string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch kind {
	case "sqlite":
		driver = sqlite.Open(t.TempDir() + "/payments.db?_pragma=busy_timeout(30000)&_txlock=immediate")
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
		schema := "payments_test_" + common.GetUUID()
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
	for _, table := range []any{&model.User{}, &model.TopUp{}} {
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

func completionForTest() domain.Completion {
	return domain.Completion{Transactions: storage.NewStore(func() *gorm.DB { return model.DB }), Math: quotaMath{}, Effects: completionEffects{}, Now: common.GetTimestamp, QuotaPerUnit: func() float64 { return common.QuotaPerUnit }}
}
func paymentMiniRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	oldSyncFrequency := common.SyncFrequency
	common.RedisEnabled = true
	common.SyncFrequency = 2
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
		common.SyncFrequency = oldSyncFrequency
	})
	return server
}

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) *model.User {
	t.Helper()
	user := &model.User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := storage.NewStore(func() *gorm.DB { return model.DB }).Find(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func createEpayTestOrder(t *testing.T, userId int, tradeNo string, provider string, status string) model.TopUp {
	t.Helper()
	topUp := model.TopUp{
		UserId:          userId,
		Amount:          2,
		Money:           10.0,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          status,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	return topUp
}

func TestRechargeEpayCreditsQuotaExactlyOnce(t *testing.T) {
	newPersistence(t, "sqlite")

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 501, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTONCE", model.PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := storage.NewStore(func() *gorm.DB { return model.DB }).Find(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)

	alreadyDone, err = completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayKeepsRedisAndDatabaseCreditInSync(t *testing.T) {
	newPersistence(t, "sqlite")
	cache := paymentMiniRedis(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 5
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 502, 7)
	_, cacheErr := model.GetUserCache(user.Id)
	require.NoError(t, cacheErr)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTREDISSYNC", model.PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 17, getUserQuotaForPaymentGuardTest(t, user.Id))
	cached, err := model.GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)
	assert.Equal(t, "17", cache.HGet(fmt.Sprintf("user:%d", user.Id), "Quota"))

	alreadyDone, err = completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	cached, err = model.GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)
	assert.Equal(t, "17", cache.HGet(fmt.Sprintf("user:%d", user.Id), "Quota"))
}

func TestRechargeEpayUpdatesPaymentMethodToActual(t *testing.T) {
	newPersistence(t, "sqlite")

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 503, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTMETHOD", model.PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "wxpay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.False(t, alreadyDone)

	reloaded := storage.NewStore(func() *gorm.DB { return model.DB }).Find(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, "wxpay", reloaded.PaymentMethod)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayRejectsForeignAndNonPendingOrders(t *testing.T) {
	newPersistence(t, "sqlite")

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 504, 7)

	t.Run("order from another payment provider", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTSTRIPE", model.PaymentProviderStripe, common.TopUpStatusPending)
		_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
		assert.ErrorIs(t, err, domain.ErrProviderMismatch)
		assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("order that is not pending", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTEXPIRED", model.PaymentProviderEpay, common.TopUpStatusExpired)
		_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
		assert.ErrorIs(t, err, domain.ErrNotPending)
		assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("missing order", func(t *testing.T) {
		_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: "EPAYTESTMISSING", Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
		assert.ErrorIs(t, err, domain.ErrNotFound)
	})
}

func TestRechargeEpayRejectsQuotaOverflowBeforeCompletingOrder(t *testing.T) {
	newPersistence(t, "sqlite")

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = float64(common.MaxWalletQuota + 1)
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 505, 3)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTOVERFLOW", model.PaymentProviderEpay, common.TopUpStatusPending)

	_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.Error(t, err)
	assert.Equal(t, 3, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRechargeEpayEnforcesFinalWalletQuotaLimit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	testCases := []struct {
		name         string
		currentQuota int
		wantErr      bool
		wantQuota    int
		wantStatus   string
	}{
		{
			name:         "allows exact highest representable wallet balance",
			currentQuota: common.MaxWalletQuota - 1_000_000,
			wantQuota:    common.MaxWalletQuota,
			wantStatus:   common.TopUpStatusSuccess,
		},
		{
			name:         "rejects balance above wallet quota domain",
			currentQuota: common.MaxWalletQuota - 999_999,
			wantErr:      true,
			wantQuota:    common.MaxWalletQuota - 999_999,
			wantStatus:   common.TopUpStatusPending,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			newPersistence(t, "sqlite")
			user := insertUserForPaymentGuardTest(t, 506, tc.currentQuota)
			order := createEpayTestOrder(t, user.Id, "EPAYTESTWALLETLIMIT", model.PaymentProviderEpay, common.TopUpStatusPending)

			_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
			if tc.wantErr {
				require.ErrorIs(t, err, domain.ErrWalletLimit)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantQuota, getUserQuotaForPaymentGuardTest(t, user.Id))
			assert.Equal(t, tc.wantStatus, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
		})
	}
}
