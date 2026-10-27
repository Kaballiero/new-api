package payments

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/adapter/admission"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/payments"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/payments/admin_list_topups"
	"github.com/QuantumNous/new-api/internal/payments/create_epay_topup"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/list_own_topups"
	"github.com/QuantumNous/new-api/internal/payments/receive_stripe_webhook"
	"github.com/QuantumNous/new-api/internal/payments/receive_waffo_webhook"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
	return domain.Completion{
		Transactions: storage.NewStore(func() *gorm.DB { return model.DB }),
		Math:         quotaMath{},
		Effects:      completionEffects{},
		Now:          common.GetTimestamp,
		QuotaPerUnit: func() float64 { return common.QuotaPerUnit },
	}
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
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testRechargeEpayCreditsQuotaExactlyOnce(t, kind) })
	}
}

func testRechargeEpayCreditsQuotaExactlyOnce(t *testing.T, kind string) {
	newPersistence(t, kind)

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
	_, cacheErr := accountstorage.Cache().GetUserCache(user.Id)
	require.NoError(t, cacheErr)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTREDISSYNC", model.PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 17, getUserQuotaForPaymentGuardTest(t, user.Id))
	cached, err := accountstorage.Cache().GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)
	assert.Equal(t, "17", cache.HGet(fmt.Sprintf("user:%d", user.Id), "Quota"))

	alreadyDone, err = completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	cached, err = accountstorage.Cache().GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)
	assert.Equal(t, "17", cache.HGet(fmt.Sprintf("user:%d", user.Id), "Quota"))
}

func TestRechargeEpayUpdatesPaymentMethodToActual(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testRechargeEpayUpdatesPaymentMethodToActual(t, kind) })
	}
}

func testRechargeEpayUpdatesPaymentMethodToActual(t *testing.T, kind string) {
	newPersistence(t, kind)

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
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testRechargeEpayRejectsForeignAndNonPendingOrders(t, kind) })
	}
}

func testRechargeEpayRejectsForeignAndNonPendingOrders(t *testing.T, kind string) {
	newPersistence(t, kind)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 504, 7)

	t.Run(
		"order from another payment provider",
		func(t *testing.T) {
			order := createEpayTestOrder(t, user.Id, "EPAYTESTSTRIPE", model.PaymentProviderStripe, common.TopUpStatusPending)
			_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
			assert.ErrorIs(t, err, domain.ErrProviderMismatch)
			assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
		},
	)

	t.Run(
		"order that is not pending",
		func(t *testing.T) {
			order := createEpayTestOrder(t, user.Id, "EPAYTESTEXPIRED", model.PaymentProviderEpay, common.TopUpStatusExpired)
			_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
			assert.ErrorIs(t, err, domain.ErrNotPending)
			assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
		},
	)

	t.Run(
		"missing order",
		func(t *testing.T) {
			_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: "EPAYTESTMISSING", Provider: domain.PaymentProviderEpay, Method: "alipay", CallerIP: "127.0.0.1"})
			assert.ErrorIs(t, err, domain.ErrNotFound)
		},
	)
}

func TestRechargeEpayRejectsQuotaOverflowBeforeCompletingOrder(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testRechargeEpayRejectsQuotaOverflowBeforeCompletingOrder(t, kind) })
	}
}

func testRechargeEpayRejectsQuotaOverflowBeforeCompletingOrder(t *testing.T, kind string) {
	newPersistence(t, kind)

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
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testRechargeEpayEnforcesFinalWalletQuotaLimit(t, kind) })
	}
}

func testRechargeEpayEnforcesFinalWalletQuotaLimit(t *testing.T, kind string) {
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
		t.Run(
			tc.name,
			func(t *testing.T) {
				newPersistence(t, kind)
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
			},
		)
	}
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &model.TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(topUp).Error)
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t, kind) })
	}
}

func testRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T, kind string) {
	newPersistence(t, kind)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, model.PaymentProviderStripe)

	_, err := completionForTest().Complete(domain.CompletionInput{TradeNo: "waffo-pancake-guard", Provider: domain.PaymentProviderWaffoPancake})
	require.Error(t, err)

	topUp := storage.NewStore(func() *gorm.DB { return model.DB }).Find("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) { testUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t, kind) })
	}
}

func testUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T, kind string) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   model.PaymentProviderCreem,
			expectedPaymentProvider: model.PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   model.PaymentProviderStripe,
			expectedPaymentProvider: model.PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(
			tc.name,
			func(t *testing.T) {
				newPersistence(t, kind)
				insertUserForPaymentGuardTest(t, 150, 0)
				insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

				err := domain.Transition(storage.NewStore(func() *gorm.DB { return model.DB }), tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
				require.ErrorIs(t, err, domain.ErrProviderMismatch)
				assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
			},
		)
	}
}

func pricingForTest() domain.Pricing {
	return domain.Pricing{Settings: settings.Settings{}, Math: quotaMath{}, Wallet: storage.Wallet{}}
}

func TestTopUpQuotaValidation(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	testCases := []struct {
		name        string
		displayType string
		amount      int64
		wantQuota   int
		wantErr     bool
	}{
		{
			name:        "currency amount below limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4294,
			wantQuota:   2_147_000_000,
		},
		{
			name:        "currency amount above limit",
			displayType: operation_setting.QuotaDisplayTypeUSD,
			amount:      4295,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount preserves settlement truncation",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      2_147_500_000,
			wantQuota:   2_147_500_000,
		},
		{
			name:        "token amount above legacy int32 range",
			displayType: operation_setting.QuotaDisplayTypeTokens,
			amount:      4_294_500_000,
			wantQuota:   4_294_500_000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.displayType
			quota, err := pricingForTest().Quota(tc.amount)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}

func TestValidateTopUpQuotaReturnsMaximumAmount(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()

	_, err := pricingForTest().Validate(maxAmount)
	require.NoError(t, err)
	_, err = pricingForTest().Validate(maxAmount + 1)
	require.EqualError(t, err, domain.MaximumAmount{Amount: maxAmount}.Error())
}

func TestRequestAmountRejectsTopUpThatCannotBeSettled(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	maxAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Floor().IntPart()
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(fmt.Sprintf(`{"amount":%d}`, maxAmount+1)),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	require.NoError(t, i18n.Init())
	NewHTTP().QuoteEpay.Handle(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(
		t,
		fmt.Sprintf(`{"message":"error","data":%q}`, i18n.Translate(i18n.DefaultLang, i18n.MsgPaymentsMaximum, map[string]any{"Amount": maxAmount})),
		recorder.Body.String(),
	)
}

func TestRequestAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		model.DB = oldDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       42,
		Username: "topup_capacity_user",
		Quota:    common.MaxWalletQuota - 100_000,
		Status:   common.UserStatusEnabled,
	}).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/amount",
		strings.NewReader(`{"amount":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	require.NoError(t, i18n.Init())
	NewHTTP().QuoteEpay.Handle(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"message":"error","data":"top-up quota limit exceeded"}`, recorder.Body.String())
}

func TestValidateCreditedQuotaRejectsOverflow(t *testing.T) {
	_, err := pricingForTest().Credited(decimal.NewFromInt(int64(common.MaxWalletQuota / 2)))
	require.NoError(t, err)
	_, err = pricingForTest().Credited(decimal.Zero)
	require.ErrorIs(t, err, domain.ErrQuotaPositive)
	_, err = pricingForTest().Credited(decimal.NewFromInt(common.MaxWalletQuota + 1))
	require.ErrorIs(t, err, domain.ErrQuotaRange)
}

func TestStripeCreditedQuotaIncludesGroupRatio(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	oldTopupGroupRatio := common.TopupGroupRatio2JSONString()
	common.QuotaPerUnit = 500000
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"vip":2}`))
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldTopupGroupRatio))
	})

	_, err := pricingForTest().Credited(pricingForTest().StripeQuota(2147, "vip"))
	require.NoError(t, err)
	_, err = pricingForTest().Credited(pricingForTest().StripeQuota(2148, "vip"))
	require.NoError(t, err)
	_, err = pricingForTest().Credited(pricingForTest().StripeQuota(int64(common.MaxWalletQuota), "vip"))
	require.Error(t, err)

	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"free":0}`))
	assert.True(t, decimal.NewFromInt(500000).Equal(pricingForTest().StripeQuota(1, "free")))
}

func TestFormatWaffoPancakeAmount_UsesDisplayPriceString(t *testing.T) {
	testCases := []struct {
		name     string
		amount   float64
		expected string
	}{
		{name: "whole amount", amount: 29, expected: "29.00"},
		{name: "decimal amount", amount: 29.9, expected: "29.90"},
		{name: "round half up to cents", amount: 29.999, expected: "30.00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, domain.PancakeAmount(tc.amount))
		})
	}
}

func TestGetWaffoPancakePayMoney(t *testing.T) {
	originalUnitPrice := setting.WaffoPancakeUnitPrice
	originalQuotaDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	maps.Copy(originalDiscounts, operation_setting.GetPaymentSetting().AmountDiscount)
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		setting.WaffoPancakeUnitPrice = originalUnitPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	setting.WaffoPancakeUnitPrice = 2.5
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{
		10:                           0.8,
		int(common.QuotaPerUnit * 3): 0.5,
		20:                           0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{
			name:             "currency display applies unit price group ratio and discount",
			amount:           10,
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         24,
		},
		{
			name:             "tokens display converts quota to display units before pricing",
			amount:           int64(common.QuotaPerUnit * 3),
			group:            "vip",
			quotaDisplayType: operation_setting.QuotaDisplayTypeTokens,
			expected:         4.5,
		},
		{
			name:             "non-positive discount falls back to no discount",
			amount:           20,
			group:            "default",
			quotaDisplayType: operation_setting.QuotaDisplayTypeUSD,
			expected:         50,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			actual := pricingForTest().DecimalMoney(tc.amount, tc.group, domain.PaymentProviderWaffoPancake)
			require.InDelta(t, tc.expected, actual, 0.000001)
		})
	}
}

func TestPaymentHistoryRetainsPartialResultsAfterPanic(t *testing.T) {
	for _, stage := range []string{"count", "find"} {
		t.Run(
			stage,
			func(t *testing.T) {
				db, _ := newPersistence(t, "sqlite")
				require.NoError(t, db.Create(&model.TopUp{UserId: 12, TradeNo: "partial-read", Status: domain.StatusPending, CreateTime: common.GetTimestamp()}).Error)
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("payments:partial", func(tx *gorm.DB) {
					if tx.Statement.Table != "top_ups" {
						return
					}
					if stage == "count" {
						if _, ok := tx.Statement.Dest.(*int64); ok {
							panic("after count scan")
						}
					}
					if stage == "find" {
						if _, ok := tx.Statement.Dest.(*[]*model.TopUp); ok {
							panic("after rows scan")
						}
					}
				}))
				t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove("payments:partial")) })
				result, err := admin_list_topups.NewUsecase(storage.NewStore(func() *gorm.DB { return db })).Execute(admin_list_topups.Input{Limit: 10})
				require.NoError(t, err)
				assert.Equal(t, int64(1), result.Total)
				if stage == "find" {
					require.Len(t, result.Orders, 1)
					assert.Equal(t, "partial-read", result.Orders[0].TradeNo)
				} else {
					assert.Nil(t, result.Orders)
				}
			},
		)
	}
}

func TestPaymentsConcurrentEpayCredit(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(
			kind,
			func(t *testing.T) {
				db, _ := newPersistence(t, kind)
				oldUnit := common.QuotaPerUnit
				common.QuotaPerUnit = 5
				t.Cleanup(func() { common.QuotaPerUnit = oldUnit })
				user := insertUserForPaymentGuardTest(t, 801, 7)
				order := createEpayTestOrder(t, user.Id, "concurrent-credit", domain.PaymentProviderEpay, domain.StatusPending)
				type outcome struct {
					duplicate bool
					err       error
				}
				ready := make(chan struct{})
				results := make(chan outcome, 2)
				for range 2 {
					go func() {
						<-ready
						duplicate, err := completionForTest().Complete(domain.CompletionInput{TradeNo: order.TradeNo, Provider: domain.PaymentProviderEpay})
						results <- outcome{duplicate: duplicate, err: err}
					}()
				}
				close(ready)
				credits := 0
				for range 2 {
					result := <-results
					if result.err == nil && !result.duplicate {
						credits++
					}
				}
				assert.Equal(t, 1, credits)
				assert.Equal(t, 17, getUserQuotaForPaymentGuardTest(t, user.Id))
				var rows int64
				require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ?", user.Id).Count(&rows).Error)
				assert.Equal(t, int64(1), rows)
				var stored model.TopUp
				require.NoError(t, db.First(&stored, order.Id).Error)
				assert.Equal(t, domain.StatusSuccess, stored.Status)
			},
		)
	}
}

func TestPaymentsDiagnosticContexts(t *testing.T) {
	for _, c := range []*gin.Context{nil, {}} {
		require.NotPanics(t, func() { observeWebhook(c, "test", []byte("test payload"), "test signature") })
		require.NotPanics(t, func() { observeEpay(c, map[string]string{"test": "fixture"}) })
		require.NotPanics(t, func() { observeEpayCheckout(c, dto.EpayRequest{}, create_epay_topup.Result{}) })
		require.NotPanics(t, func() { observeCreemPay(c, []byte("test payload")) })
	}
}

func TestPaymentsCompletionMatrix(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				name, provider string
				manual         bool
				credit         int
			}{
				{"epay", domain.PaymentProviderEpay, false, 10},
				{"stripe", domain.PaymentProviderStripe, false, 15},
				{"creem", domain.PaymentProviderCreem, false, 2},
				{"waffo", domain.PaymentProviderWaffo, false, 10},
				{"pancake", domain.PaymentProviderWaffoPancake, false, 10},
				{"manual_creem", domain.PaymentProviderCreem, true, 10},
			} {
				t.Run(tc.name, func(t *testing.T) {
					db, logs := newPersistence(t, kind)
					oldUnit := common.QuotaPerUnit
					common.QuotaPerUnit = 5
					t.Cleanup(func() { common.QuotaPerUnit = oldUnit })
					user := insertUserForPaymentGuardTest(t, 901, 7)
					order := createEpayTestOrder(t, user.Id, "provider-credit", tc.provider, domain.StatusPending)
					require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("money", 3).Error)
					in := domain.CompletionInput{TradeNo: order.TradeNo, Provider: tc.provider, Manual: tc.manual, Email: "payment@example.test", Customer: "test-customer"}
					_, err := completionForTest().Complete(in)
					require.NoError(t, err)
					assert.Equal(t, 7+tc.credit, getUserQuotaForPaymentGuardTest(t, user.Id))
					assert.Equal(t, domain.StatusSuccess, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
					_, err = completionForTest().Complete(in)
					if !tc.manual && (tc.provider == domain.PaymentProviderStripe || tc.provider == domain.PaymentProviderCreem) {
						require.ErrorIs(t, err, domain.ErrRecharge)
					} else {
						require.NoError(t, err)
					}
					assert.Equal(t, 7+tc.credit, getUserQuotaForPaymentGuardTest(t, user.Id))
					var count int64
					require.NoError(t, logs.Model(&model.Log{}).Count(&count).Error)
					expectedLogs := int64(1)
					if tc.manual {
						expectedLogs = 2
					}
					assert.Equal(t, expectedLogs, count)
					require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", common.MaxWalletQuota-1).Error)
					overflow := createEpayTestOrder(t, user.Id, "provider-overflow", tc.provider, domain.StatusPending)
					require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", overflow.Id).Update("money", 3).Error)
					in.TradeNo = overflow.TradeNo
					_, err = completionForTest().Complete(in)
					require.Error(t, err)
					assert.Equal(t, common.MaxWalletQuota-1, getUserQuotaForPaymentGuardTest(t, user.Id))
					assert.Equal(t, domain.StatusPending, getTopUpStatusForPaymentGuardTest(t, overflow.TradeNo))
					require.NoError(t, logs.Model(&model.Log{}).Count(&count).Error)
					assert.Equal(t, expectedLogs, count)
				})
			}
		})
	}
}

func TestPaymentsHistoryQueries(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			db, _ := newPersistence(t, kind)
			now := common.GetTimestamp()
			rows := []model.TopUp{
				{UserId: 12, TradeNo: "literal_!", CreateTime: now, Status: domain.StatusPending},
				{UserId: 12, TradeNo: "old", CreateTime: now - 31*24*60*60, Status: domain.StatusSuccess},
				{UserId: 13, TradeNo: "foreign", CreateTime: now, Status: domain.StatusPending},
			}
			require.NoError(t, db.Create(&rows).Error)
			store := storage.NewStore(func() *gorm.DB { return db })
			own, err := list_own_topups.NewUsecase(store, func() int64 { return now }).Execute(list_own_topups.Input{UserID: 12, Keyword: "literal_!", Limit: 10})
			require.NoError(t, err)
			require.Len(t, own.Orders, 1)
			assert.Equal(t, int64(1), own.Total)
			assert.Equal(t, rows[0].Id, own.Orders[0].ID)
			all, err := admin_list_topups.NewUsecase(store).Execute(admin_list_topups.Input{Limit: 10})
			require.NoError(t, err)
			require.Len(t, all.Orders, 3)
			assert.Equal(t, int64(3), all.Total)
			assert.Equal(t, rows[2].Id, all.Orders[0].ID)
		})
	}
}

// Direct handler coverage reaches the read-failure branch hidden by the routed body limiter.
func TestCreemPayReadFailure(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct{ lang, want string }{
		{"en", "read query error"}, {"ru", "Не удалось прочитать тело запроса"}, {"zh-CN", "read query error"}, {"zh-TW", "read query error"}, {"xx-YY", "read query error"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/user/creem/pay", iotest.ErrReader(errors.New("fixture body read failure")))
			c.Request.Header.Set("Accept-Language", tc.lang)
			NewHTTP().CreateCreem.Handle(c)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, fmt.Sprintf(`{"message":"error","data":%q}`, tc.want), recorder.Body.String())
		})
	}
}

type invalidWaffoFactory struct{}

func (invalidWaffoFactory) Client() (domain.WaffoClient, error) {
	return nil, errors.New("fixture invalid SDK configuration")
}

type waffoObservationCount struct{ calls int }

func (o *waffoObservationCount) Received(ctx context.Context, in receive_waffo_webhook.Input) {
	o.calls++
}

func TestWaffoInvalidConfigurationDoesNotLogPayload(t *testing.T) {
	observation := &waffoObservationCount{}
	usecase := receive_waffo_webhook.NewUsecase(invalidWaffoFactory{}, nil, domain.Completion{}, nil, observation, paymentDiagnostics{})
	_, err := usecase.Execute(context.Background(), receive_waffo_webhook.Input{Payload: []byte("test payload"), Signature: "test signature"})
	require.ErrorIs(t, err, domain.ErrPaymentConfig)
	assert.Zero(t, observation.calls)
	require.NotPanics(t, func() { waffoObservation{}.Received(nil, receive_waffo_webhook.Input{Payload: []byte("test payload")}) })
}

// diagnosticCapture observes the operational contract independently of the log transport.
type diagnosticCapture struct{ entries []string }

func (d *diagnosticCapture) Info(ctx context.Context, format string, values ...any) {
	d.entries = append(d.entries, "info: "+fmt.Sprintf(format, values...))
}

func (d *diagnosticCapture) Warn(ctx context.Context, format string, values ...any) {
	d.entries = append(d.entries, "warn: "+fmt.Sprintf(format, values...))
}

func (d *diagnosticCapture) Error(ctx context.Context, format string, values ...any) {
	d.entries = append(d.entries, "error: "+fmt.Sprintf(format, values...))
}

type stripeEventFixture struct{ event domain.StripeEvent }

func (s stripeEventFixture) Checkout(in domain.StripeCheckout) (string, error) { return "", nil }

func (s stripeEventFixture) Verify(payload []byte, signature string) (domain.StripeEvent, error) {
	return s.event, nil
}

type subscriptionFailureFixture struct{ err error }

func (s subscriptionFailureFixture) Complete(tradeNo, payload, provider string) error { return s.err }

func (s subscriptionFailureFixture) Expire(tradeNo, provider string) error { return s.err }

func (s subscriptionFailureFixture) Find(tradeNo string) *domain.Order { return nil }

func TestPaymentStripeFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, eventType, status, expected string
		subscriptionError                 error
		failSave                          bool
	}{
		{"subscription_completion", "checkout.session.completed", "complete", "error: Stripe subscription order processing failed trade_no=diagnostic event_type=checkout.session.completed client_ip=fixture error=\"subscription failed\"", errors.New("subscription failed"), false},
		{"subscription_expiry", "checkout.session.expired", "expired", "error: Stripe subscription order expiry failed trade_no=diagnostic error=\"expiry failed\"", errors.New("expiry failed"), false},
		{"async_failed_save", "checkout.session.async_payment_failed", "", "error: Stripe failed to mark topup order failed trade_no=diagnostic client_ip=fixture error=\"write failed\"", domain.ErrSubscriptionNotFound, true},
		{"topup_expiry_save", "checkout.session.expired", "expired", "error: Stripe topup order expiry failed trade_no=diagnostic error=\"write failed\"", domain.ErrSubscriptionNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := newPersistence(t, "sqlite")
			store := storage.NewStore(func() *gorm.DB { return db })
			order := domain.Order{UserID: 12, TradeNo: "diagnostic", Status: domain.StatusPending, PaymentProvider: domain.PaymentProviderStripe}
			require.NoError(t, store.Insert(&order))
			if tc.failSave {
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register("payments:diagnostic", func(tx *gorm.DB) {
					tx.AddError(errors.New("write failed"))
				}))
				t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove("payments:diagnostic")) })
			}
			capture := &diagnosticCapture{}
			client := stripeEventFixture{event: domain.StripeEvent{Type: tc.eventType, Reference: "diagnostic", Status: tc.status, PaymentStatus: "paid"}}
			usecase := receive_stripe_webhook.NewUsecase(client, store, store, subscriptionFailureFixture{err: tc.subscriptionError}, domain.Completion{}, &admission.OrderAdmission{}, capture)
			require.NoError(t, usecase.Execute(context.Background(), receive_stripe_webhook.Input{CallerIP: "fixture"}))
			assert.Contains(t, capture.entries, tc.expected)
			assert.Equal(t, domain.StatusPending, store.Find("diagnostic").Status)
		})
	}
}

type completionFailureFixture struct{}

func (completionFailureFixture) Transaction(run func(tx domain.Tx) error) error {
	return errors.New("credit failed")
}

type completionEffectCapture struct{ failed, synced string }

func (e *completionEffectCapture) Failed(operation string, err error) {
	e.failed = operation + " failed: " + err.Error()
}

func (e *completionEffectCapture) Sync(userID, quota int, operation string) { e.synced = operation }

func (e *completionEffectCapture) Receipt(order domain.Order, quota int, ip, provider string) {}

func TestPaymentCompletionOperationalMessages(t *testing.T) {
	assert.Equal(t, "topup failed, please try again later", paymentLogValues([]any{domain.ErrRecharge})[0])
	assert.Equal(t, "topup order not found", paymentLogValues([]any{domain.ErrOrderMissing})[0])
	for _, tc := range []struct{ provider, failure, sync string }{
		{domain.PaymentProviderEpay, "epay topup failed: credit failed", "epay topup"},
		{domain.PaymentProviderStripe, "topup failed: credit failed", "stripe topup"},
		{domain.PaymentProviderCreem, "creem topup failed: credit failed", "creem topup"},
		{domain.PaymentProviderWaffo, "waffo topup failed: credit failed", "waffo topup"},
		{domain.PaymentProviderWaffoPancake, "waffo pancake topup failed: credit failed", "waffo pancake topup"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			effects := &completionEffectCapture{}
			completion := domain.Completion{Transactions: completionFailureFixture{}, Effects: effects}
			_, err := completion.Complete(domain.CompletionInput{TradeNo: "failure", Provider: tc.provider})
			require.Error(t, err)
			assert.Equal(t, tc.failure, effects.failed)
			assert.Empty(t, effects.synced)

			db, _ := newPersistence(t, "sqlite")
			user := model.User{Username: "diagnostic", AffCode: "diagnostic"}
			require.NoError(t, db.Create(&user).Error)
			store := storage.NewStore(func() *gorm.DB { return db })
			order := domain.Order{UserID: user.Id, TradeNo: "success", Amount: 2, Money: 2, PaymentProvider: tc.provider, Status: domain.StatusPending}
			require.NoError(t, store.Insert(&order))
			completion.Transactions, completion.Math, completion.Now, completion.QuotaPerUnit = store, quotaMath{}, func() int64 { return 1 }, func() float64 { return 1 }
			_, err = completion.Complete(domain.CompletionInput{TradeNo: "success", Provider: tc.provider})
			require.NoError(t, err)
			assert.Equal(t, tc.sync, effects.synced)
		})
	}
}

func TestPaymentReceiptLanguages(t *testing.T) {
	db, _ := newPersistence(t, "sqlite")
	for i, tc := range []struct{ raw, want string }{
		{"", "en"}, {"en-US", "en"}, {"ru", "ru"}, {"ru-RU", "ru"}, {"zh", "zh-CN"}, {"zh-CN", "zh-CN"}, {"zh-TW", "zh-CN"}, {"zh-Hant", "zh-CN"}, {"zh-Hans", "zh-CN"}, {"unknown", "en"},
	} {
		user := model.User{Id: 1000 + i, Username: fmt.Sprintf("language-%d", i), AffCode: fmt.Sprintf("lang-%d", i), Setting: fmt.Sprintf(`{"language":%q}`, tc.raw)}
		require.NoError(t, db.Create(&user).Error)
		assert.Equal(t, tc.want, paymentReceiptLanguage(user.Id), tc.raw)
	}
	oldDisplay := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldSymbol := operation_setting.GetGeneralSetting().CustomCurrencySymbol
	oldRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.GetGeneralSetting().CustomCurrencySymbol = oldSymbol
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = oldRate
	})
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCustom
	operation_setting.GetGeneralSetting().CustomCurrencySymbol = "\uff04"
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 1
	for _, lang := range []string{"en", "ru"} {
		assert.Contains(t, paymentQuotaText(100, lang, false), "\uff04")
		assert.Contains(t, paymentQuotaText(100, lang, true), "\uff04")
	}
}
