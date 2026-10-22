package billing

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/billing/create_reservation"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/billing/settle_reservation"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBillingAdapterPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("host-only: TEST_POSTGRES_DSN is not configured")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	admin := stdlib.OpenDB(*config.Copy())
	t.Cleanup(func() { assert.NoError(t, admin.Close()) })

	// Never migrate or write fixture rows in a schema shared with other packages.
	schema := "wave2b1_" + common.GetUUID()
	identifier := pgx.Identifier{schema}.Sanitize()
	_, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+identifier)
	require.NoError(t, err)
	t.Cleanup(func() {
		// t.Context is already cancelled when cleanup starts.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+identifier+" CASCADE")
		assert.NoError(t, err)
	})

	// Startup parameters isolate every pooled connection, including transactions
	// opened by model primitives; a one-connection SET search_path is insufficient.
	config.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*config)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	require.NoError(t, err)
	var currentSchema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&currentSchema).Error)
	require.Equal(t, schema, currentSchema)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 5*time.Second, 10*time.Millisecond)
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
	})
	var version string
	require.NoError(t, db.Raw("SHOW server_version").Scan(&version).Error)
	t.Logf("PostgreSQL %s, isolated schema %s", version, schema)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
	user := model.User{Username: "wave2b1-postgres", Quota: 1000, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "wave2b1-postgres", RemainQuota: 1000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(&token).Error)
	store := Store{}
	input := create_reservation.Input{Account: domain.Account{UserID: 1, TokenID: 1, TokenKey: "fixture"}, RequestID: "request", Quota: 100, Preference: "wallet_only"}
	input.Account = domain.Account{UserID: user.Id, TokenID: token.Id, TokenKey: token.Key}
	r, err := create_reservation.NewUsecase(store, func() int { return 0 }, func(string) {}).Execute(input)
	require.NoError(t, err)
	require.NoError(t, settle_reservation.NewUsecase(store, func(string) {}).Execute(settle_reservation.Input{Reservation: r, Actual: 150}))
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, 850, user.Quota)
	assert.Equal(t, 850, token.RemainQuota)
	assert.Equal(t, 150, token.UsedQuota)
	require.NoError(t, db.Model(&user).Update("batch_held_quota", 800).Error)
	_, err = create_reservation.NewUsecase(store, func() int { return 0 }, func(string) {}).Execute(input)
	require.Error(t, err)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, 850, user.Quota)
	assert.Equal(t, 850, token.RemainQuota, "failed wallet reserve rolls the token leg back")
	plan := model.SubscriptionPlan{Title: "wave2b1-postgres", TotalAmount: 1000, QuotaResetPeriod: "never"}
	require.NoError(t, db.Create(&plan).Error)
	sub := model.UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: 1000, Status: "active", StartTime: 1700000000, EndTime: 4102444800}
	require.NoError(t, db.Create(&sub).Error)
	input.Preference, input.RequestID = "subscription_only", "wave2b1-postgres-request"
	r, err = create_reservation.NewUsecase(store, func() int { return 0 }, func(string) {}).Execute(input)
	require.NoError(t, err)
	require.NoError(t, settle_reservation.NewUsecase(store, func(string) {}).Execute(settle_reservation.Input{Reservation: r, Actual: 50}))
	require.NoError(t, db.First(&sub, sub.Id).Error)
	assert.Equal(t, int64(50), sub.AmountUsed)
}
