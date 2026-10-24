package subscriptions_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/subscriptions"
	subscriptionsapp "github.com/QuantumNous/new-api/internal/app/subscriptions"
	"github.com/QuantumNous/new-api/internal/subscriptions/bind_subscription"
	"github.com/QuantumNous/new-api/internal/subscriptions/complete_order"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/invalidate_subscription"
	"github.com/QuantumNous/new-api/internal/subscriptions/reset_subscriptions"
	. "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func subscriptionFixture(t *testing.T) storage.Store {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	oldPath := common.SQLitePath
	t.Setenv("SQL_DSN", "")
	common.SQLitePath = t.TempDir() + "/main.db"
	require.NoError(t, InitDB())
	common.SQLitePath = oldPath
	db := DB
	var err error
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&User{}, &UserSession{}, &SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}, &TopUp{}, &Log{}))
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(4)
	oldRedis, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.RDB = oldRedis, oldRDB
		_ = pool.Close()
	})
	return storage.NewStore(func() *gorm.DB { return DB })
}
func seedSubscriptionResetPlan(t *testing.T, plan *SubscriptionPlan) {
	t.Helper()
	require.NoError(t, DB.Create(plan).Error)
}

func seedSubscriptionResetSub(t *testing.T, sub *UserSubscription) {
	t.Helper()
	require.NoError(t, DB.Create(sub).Error)
}

func getSubscriptionResetSub(t *testing.T, id int) UserSubscription {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", id).First(&sub).Error)
	return sub
}

func TestAdminResetUserSubscriptionsByPlanResetsAllActiveMatchesAndAdvancesTime(t *testing.T) {
	store := subscriptionFixture(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9101,
		Title:            "Pro",
		PriceAmount:      10,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      1000,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	otherPlan := &SubscriptionPlan{
		Id:               9102,
		Title:            "Basic",
		PriceAmount:      1,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      100,
		QuotaResetPeriod: SubscriptionResetDaily,
	}
	seedSubscriptionResetPlan(t, plan)
	seedSubscriptionResetPlan(t, otherPlan)

	activeEnd := now + 30*24*3600
	expiredEnd := now - 1
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9201, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 300, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9202, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 500, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9203, UserId: 101, PlanId: otherPlan.Id, AmountTotal: 100, AmountUsed: 60, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9204, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 700, StartTime: now - 7200, EndTime: expiredEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now - 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9205, UserId: 102, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 800, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 120})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9206, UserId: 101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 3600, EndTime: activeEnd, Status: "cancelled", LastResetTime: now - 3600, NextResetTime: now + 120})

	beforeReset := GetDBTimestamp()
	result, err := reset_subscriptions.NewUsecase(store, GetDBTimestamp).Execute(reset_subscriptions.Input{UserID: 101, PlanID: plan.Id, AdvanceResetTime: true})
	afterReset := GetDBTimestamp()

	require.NoError(t, err)
	require.NotZero(t, result.PlanId)
	assert.Equal(t, plan.Id, result.PlanId)
	assert.Equal(t, 2, result.MatchedCount)
	assert.Equal(t, 2, result.ResetCount)
	assert.Equal(t, 1, result.UserCount)
	assert.Equal(t, []int{101}, result.AffectedUserIds)
	assert.True(t, result.AdvanceResetTime)

	for _, id := range []int{9201, 9202} {
		sub := getSubscriptionResetSub(t, id)
		assert.Zero(t, sub.AmountUsed)
		assert.GreaterOrEqual(t, sub.LastResetTime, beforeReset)
		assert.LessOrEqual(t, sub.LastResetTime, afterReset)
		assert.Equal(t, domain.NextResetTime(time.Unix(sub.LastResetTime, 0), (*domain.SubscriptionPlan)(plan), sub.EndTime), sub.NextResetTime)
	}
	assert.EqualValues(t, 60, getSubscriptionResetSub(t, 9203).AmountUsed)
	assert.EqualValues(t, 700, getSubscriptionResetSub(t, 9204).AmountUsed)
	assert.EqualValues(t, 800, getSubscriptionResetSub(t, 9205).AmountUsed)
	assert.EqualValues(t, 900, getSubscriptionResetSub(t, 9206).AmountUsed)
}

func TestAdminResetUserSubscriptionsByPlanKeepsResetTimes(t *testing.T) {
	store := subscriptionFixture(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9301,
		Title:            "Team",
		PriceAmount:      20,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      2000,
		QuotaResetPeriod: SubscriptionResetMonthly,
	}
	seedSubscriptionResetPlan(t, plan)

	lastReset := now - 86400
	nextReset := now + 86400
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9302, UserId: 201, PlanId: plan.Id, AmountTotal: 2000, AmountUsed: 1200, StartTime: now - 172800, EndTime: now + 30*24*3600, Status: "active", LastResetTime: lastReset, NextResetTime: nextReset})

	result, err := reset_subscriptions.NewUsecase(store, GetDBTimestamp).Execute(reset_subscriptions.Input{UserID: 201, PlanID: plan.Id, AdvanceResetTime: false})

	require.NoError(t, err)
	assert.False(t, result.AdvanceResetTime)
	sub := getSubscriptionResetSub(t, 9302)
	assert.Zero(t, sub.AmountUsed)
	assert.Equal(t, lastReset, sub.LastResetTime)
	assert.Equal(t, nextReset, sub.NextResetTime)
}

func TestAdminResetUserSubscriptionsByPlanNoActiveMatchReturnsError(t *testing.T) {
	store := subscriptionFixture(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:            9401,
		Title:         "Expired",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
	}
	seedSubscriptionResetPlan(t, plan)
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9402, UserId: 301, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 500, StartTime: now - 7200, EndTime: now - 1, Status: "active"})

	result, err := reset_subscriptions.NewUsecase(store, GetDBTimestamp).Execute(reset_subscriptions.Input{UserID: 301, PlanID: plan.Id, AdvanceResetTime: true})

	require.Error(t, err)
	assert.Zero(t, result.PlanId)
	assert.True(t, strings.Contains(err.Error(), "该用户没有有效的此套餐订阅"))
}

func TestAdminResetPlanSubscriptionsResetsAllActiveUsers(t *testing.T) {
	store := subscriptionFixture(t)

	now := GetDBTimestamp()
	plan := &SubscriptionPlan{
		Id:               9501,
		Title:            "Business",
		PriceAmount:      30,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      3000,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	seedSubscriptionResetPlan(t, plan)

	activeEnd := now + 30*24*3600
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9502, UserId: 401, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1000, StartTime: now - 3600, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9503, UserId: 401, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1100, StartTime: now - 3500, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9504, UserId: 402, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1200, StartTime: now - 3400, EndTime: activeEnd, Status: "active", LastResetTime: now - 3600, NextResetTime: now + 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9505, UserId: 403, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1300, StartTime: now - 7200, EndTime: now - 1, Status: "active", LastResetTime: now - 3600, NextResetTime: now - 10})
	seedSubscriptionResetSub(t, &UserSubscription{Id: 9506, UserId: 404, PlanId: plan.Id, AmountTotal: 3000, AmountUsed: 1400, StartTime: now - 3600, EndTime: activeEnd, Status: "cancelled", LastResetTime: now - 3600, NextResetTime: now + 10})

	result, err := reset_subscriptions.NewUsecase(store, GetDBTimestamp).Execute(reset_subscriptions.Input{PlanID: plan.Id, AdvanceResetTime: true})

	require.NoError(t, err)
	require.NotZero(t, result.PlanId)
	assert.Equal(t, 3, result.MatchedCount)
	assert.Equal(t, 3, result.ResetCount)
	assert.Equal(t, 2, result.UserCount)
	assert.Equal(t, []int{401, 402}, result.AffectedUserIds)
	for _, id := range []int{9502, 9503, 9504} {
		sub := getSubscriptionResetSub(t, id)
		assert.Zero(t, sub.AmountUsed)
		assert.Zero(t, sub.LastResetTime)
		assert.Zero(t, sub.NextResetTime)
	}
	assert.EqualValues(t, 1300, getSubscriptionResetSub(t, 9505).AmountUsed)
	assert.EqualValues(t, 1400, getSubscriptionResetSub(t, 9506).AmountUsed)
}

func TestAdminResetPlanSubscriptionsNoMatchSucceeds(t *testing.T) {
	store := subscriptionFixture(t)

	plan := &SubscriptionPlan{
		Id:            9601,
		Title:         "Empty",
		PriceAmount:   10,
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   1000,
	}
	seedSubscriptionResetPlan(t, plan)

	result, err := reset_subscriptions.NewUsecase(store, GetDBTimestamp).Execute(reset_subscriptions.Input{PlanID: plan.Id, AdvanceResetTime: true})

	require.NoError(t, err)
	require.NotZero(t, result.PlanId)
	assert.Zero(t, result.MatchedCount)
	assert.Zero(t, result.ResetCount)
	assert.Zero(t, result.UserCount)
	assert.Empty(t, result.AffectedUserIds)
}

func TestSubscriptionGroupTransitionsPreserveAuthVersionAndSessions(t *testing.T) {
	store := subscriptionFixture(t)
	mini := miniredis.RunT(t)
	oldRedis, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = common.RDB.Close(); common.RedisEnabled, common.RDB = oldRedis, oldRDB })
	now := time.Now().Unix()
	user := User{
		Username:    "subscription-auth-user",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, CreateUserSession(&UserSession{
		SID:             "subscription-auth-session",
		UserID:          user.Id,
		Version:         1,
		UserAuthVersion: 1,
		Status:          UserSessionStatusActive,
		RefreshHash:     "refresh-hash",
		LoginMethod:     "password",
		LastActiveAt:    now,
		ExpiresAt:       now + 3600,
	}))
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	plan := &SubscriptionPlan{
		Title:         "Upgraded",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)

	_, err = bind_subscription.NewUsecase(store, common.GetTimestamp, func(id int, _ string) { require.NoError(t, RefreshUserGroupCache(id)) }).Execute(bind_subscription.Input{UserID: user.Id, PlanID: plan.Id})
	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	require.NoError(t, err)
	require.Equal(t, "default", subscription.PrevUserGroup)
	require.NoError(t, RefreshUserGroupCache(user.Id))

	var updated User
	require.NoError(t, DB.First(&updated, user.Id).Error)
	assert.Equal(t, "pro", updated.Group)
	assert.EqualValues(t, 1, updated.AuthVersion)
	var session UserSession
	require.NoError(t, DB.First(&session, "sid = ?", "subscription-auth-session").Error)
	assert.Equal(t, UserSessionStatusActive, session.Status)
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "pro", cached.Group)
	assert.EqualValues(t, 1, cached.AuthVersion)

	target, err := invalidate_subscription.NewUsecase(store, common.GetTimestamp, func(id int, _ string) { require.NoError(t, RefreshUserGroupCache(id)) }).Execute(subscription.Id)
	require.NoError(t, err)
	assert.Equal(t, "default", target)
	require.NoError(t, RefreshUserGroupCache(user.Id))
	require.NoError(t, DB.First(&updated, user.Id).Error)
	assert.Equal(t, "default", updated.Group)
	assert.EqualValues(t, 1, updated.AuthVersion)
	require.NoError(t, DB.First(&session, "sid = ?", "subscription-auth-session").Error)
	assert.Equal(t, UserSessionStatusActive, session.Status)
	cached, err = GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "default", cached.Group)
}

func TestSubscriptionGroupCacheRefreshFailureDoesNotChangeCommittedResult(t *testing.T) {
	subscriptionFixture(t)
	previousDB, previousLogDB := DB, LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&User{}, &SubscriptionPlan{}, &UserSubscription{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		_ = sqlDB.Close()
	})

	user := User{
		Username:    "subscription-cache-failure",
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, DB.Create(&user).Error)
	plan := &SubscriptionPlan{
		Title:         "Cache failure plan",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		TotalAmount:   100,
		UpgradeGroup:  "pro",
		Enabled:       true,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("forced redis failure")
		},
		MaxRetries: -1,
	})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})

	result, err := bind_subscription.NewUsecase(storage.NewStore(func() *gorm.DB { return DB }), common.GetTimestamp, func(id int, operation string) {
		if err := RefreshUserGroupCache(id); err != nil {
			common.SysError(fmt.Sprintf("failed to refresh user group cache after %s for user %d: %v", operation, id, err))
		}
	}).Execute(bind_subscription.Input{UserID: user.Id, PlanID: plan.Id})
	require.NoError(t, err)
	assert.Equal(t, "pro", result.UpgradeGroup)

	var updated User
	require.NoError(t, DB.First(&updated, user.Id).Error)
	assert.Equal(t, "pro", updated.Group)
	assert.EqualValues(t, 1, updated.AuthVersion)
	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	assert.Equal(t, "active", subscription.Status)
}

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) *User {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, DB.Create(order).Error)
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	subscriptionFixture(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := subscriptionsapp.NewOrders().Complete(complete_order.Input{TradeNo: "sub-guard-order", ProviderPayload: `{"provider":"epay"}`, ExpectedProvider: PaymentProviderEpay, ActualMethod: "alipay"})
	require.ErrorIs(t, err, domain.ErrProviderMismatch)

	order := subscriptionsapp.NewOrders().Find("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	var count int64
	require.NoError(t, DB.Model(&TopUp{}).Where("trade_no = ?", "sub-guard-order").Count(&count).Error)
	assert.Zero(t, count)
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	subscriptionFixture(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := subscriptionsapp.NewOrders().Expire("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, domain.ErrProviderMismatch)

	order := subscriptionsapp.NewOrders().Find("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}

// Every PostgreSQL fixture owns its schema; startup parameters apply to every pooled connection.
func subscriptionPostgres(t *testing.T) storage.Store {
	t.Helper()
	subscriptionFixture(t) // Initialize the existing legacy dialect-dependent cache columns.
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	admin := stdlib.OpenDB(*cfg)
	schema := "subscriptions_test_" + common.GetUUID()
	quoted := pgx.Identifier{schema}.Sanitize()
	_, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+quoted)
	require.NoError(t, err)
	cfg.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*cfg)
	t.Cleanup(func() {
		assert.NoError(t, pool.Close())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+quoted+" CASCADE")
		assert.NoError(t, err)
		assert.NoError(t, admin.Close())
	})
	first, err := pool.Conn(t.Context())
	require.NoError(t, err)
	defer first.Close()
	second, err := pool.Conn(t.Context())
	require.NoError(t, err)
	defer second.Close()
	for _, connection := range []*sql.Conn{first, second} {
		var current string
		require.NoError(t, connection.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&current))
		require.Equal(t, schema, current)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	require.NoError(t, db.AutoMigrate(&User{}, &SubscriptionPlan{}, &SubscriptionOrder{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &TopUp{}, &Log{}))
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Logf("database version: %s; isolated schema: %s", version, schema)
	return storage.NewStore(func() *gorm.DB { return DB })
}

func TestSubscriptionsPersistence(t *testing.T) {
	for _, kind := range []string{"sqlite", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			var store storage.Store
			if kind == "postgres" {
				store = subscriptionPostgres(t)
			} else {
				store = subscriptionFixture(t)
				var version string
				require.NoError(t, DB.Raw("SELECT sqlite_version()").Scan(&version).Error)
				t.Logf("database version: SQLite %s", version)
			}
			require.NoError(t, i18n.Init())
			user := User{Username: "subscription-matrix", AffCode: "matrix-first", Password: "hash", Group: "default", Quota: 30, Status: common.UserStatusEnabled, AuthVersion: 1}
			require.NoError(t, DB.Create(&user).Error)
			plan := SubscriptionPlan{Title: "Matrix", PriceAmount: 2.01, Enabled: true, DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 100, QuotaResetPeriod: SubscriptionResetDaily, UpgradeGroup: "pro", MaxPurchasePerUser: 2}
			require.NoError(t, DB.Create(&plan).Error)
			InvalidateSubscriptionPlanCache(plan.Id)
			order := SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: "matrix-order", PaymentMethod: "wechat", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp()}
			require.NoError(t, DB.Create(&order).Error)
			orders := subscriptionsapp.NewOrders()
			input := complete_order.Input{TradeNo: order.TradeNo, ExpectedProvider: PaymentProviderEpay, ActualMethod: "alipay"}
			require.NoError(t, orders.Complete(input))
			require.NoError(t, orders.Complete(input))
			var subs []UserSubscription
			require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&subs).Error)
			require.Len(t, subs, 1)
			var history TopUp
			require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&history).Error)
			assert.Equal(t, "wechat", history.PaymentMethod)
			assert.Empty(t, history.PaymentProvider)
			assert.Equal(t, "alipay", orders.Find(order.TradeNo).PaymentMethod)
			require.NoError(t, DB.First(&user, user.Id).Error)
			assert.Equal(t, "pro", user.Group)
			assert.EqualValues(t, 1, user.AuthVersion)
			require.NoError(t, DB.Model(&subs[0]).Update("amount_used", 70).Error)
			reset, err := reset_subscriptions.NewUsecase(store, GetDBTimestamp).Execute(reset_subscriptions.Input{UserID: user.Id, PlanID: plan.Id, AdvanceResetTime: true})
			require.NoError(t, err)
			assert.Equal(t, 1, reset.ResetCount)
			require.NoError(t, DB.First(&subs[0], subs[0].Id).Error)
			assert.Zero(t, subs[0].AmountUsed)
			group, err := invalidate_subscription.NewUsecase(store, common.GetTimestamp, func(int, string) {}).Execute(subs[0].Id)
			require.NoError(t, err)
			assert.Equal(t, "default", group)
			// Balance uses the production composition, decimal ceil, and no history projection.
			oldQuota := common.QuotaPerUnit
			oldPayment := *operation_setting.GetPaymentSetting()
			common.QuotaPerUnit = 10
			*operation_setting.GetPaymentSetting() = operation_setting.PaymentSetting{ComplianceConfirmed: true, ComplianceTermsVersion: operation_setting.CurrentComplianceTermsVersion}
			t.Cleanup(func() { common.QuotaPerUnit = oldQuota; *operation_setting.GetPaymentSetting() = oldPayment })
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("id", user.Id)
			c.Request = httptest.NewRequest("POST", "/api/subscription/balance/pay", strings.NewReader(fmt.Sprintf(`{"plan_id":%d}`, plan.Id)))
			c.Request.Header.Set("Content-Type", "application/json")
			subscriptionsapp.NewHTTP().Balance.Handle(c)
			assert.Contains(t, recorder.Body.String(), `"success":true`)
			require.NoError(t, DB.First(&user, user.Id).Error)
			assert.Equal(t, 9, user.Quota)
			var historyCount int64
			require.NoError(t, DB.Model(&TopUp{}).Count(&historyCount).Error)
			assert.EqualValues(t, 1, historyCount)
			// A conflicting existing history method rolls back grant and group changes together.
			other := User{Username: "subscription-matrix-rollback", AffCode: "matrix-second", Password: "hash", Group: "default", Status: common.UserStatusEnabled}
			require.NoError(t, DB.Create(&other).Error)
			failed := SubscriptionOrder{UserId: other.Id, PlanId: plan.Id, Money: 2.01, TradeNo: "matrix-conflict", PaymentMethod: "wechat", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}
			require.NoError(t, DB.Create(&failed).Error)
			conflict := TopUp{UserId: other.Id, TradeNo: failed.TradeNo, PaymentMethod: "stripe", Status: common.TopUpStatusPending}
			require.NoError(t, DB.Create(&conflict).Error)
			require.ErrorIs(t, orders.Complete(complete_order.Input{TradeNo: failed.TradeNo, ExpectedProvider: PaymentProviderEpay}), domain.ErrProviderMismatch)
			var grants int64
			require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", other.Id).Count(&grants).Error)
			assert.Zero(t, grants)
			require.NoError(t, DB.First(&other, other.Id).Error)
			assert.Equal(t, "default", other.Group)
			assert.Equal(t, common.TopUpStatusPending, orders.Find(failed.TradeNo).Status)
		})
	}
}
