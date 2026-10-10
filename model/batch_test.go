package model

import (
	"github.com/glebarez/sqlite"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestBatchDurableAccounting(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openModelCostFXTestDB(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_contract_"}
			previousDB, previousRedis, previousRDB, previousBatch := DB, common.RedisEnabled, common.RDB, common.BatchUpdateEnabled
			DB = db
			previousLogDB := LOG_DB
			LOG_DB = db
			t.Cleanup(func() { LOG_DB = previousLogDB })
			previousKeyCol := commonKeyCol
			commonKeyCol = "`key`"
			if dialect == "postgres" {
				commonKeyCol = `"key"`
			}
			t.Cleanup(func() { commonKeyCol = previousKeyCol })
			common.RedisEnabled = false
			common.BatchUpdateEnabled = false
			t.Cleanup(func() {
				DB = previousDB
				common.RedisEnabled = previousRedis
				common.RDB = previousRDB
				common.BatchUpdateEnabled = previousBatch
			})
			models := []any{&User{}, &Token{}, &Channel{}, &BatchJob{}, &BatchItem{}, &BatchQuotaEvent{}, &BatchLogEvent{}, &BatchLogReceipt{}, &Log{}}
			require.NoError(t, db.AutoMigrate(models...))
			t.Cleanup(func() {
				for i := len(models) - 1; i >= 0; i-- {
					_ = db.Migrator().DropTable(models[i])
				}
			})
			user := createReserveTestUser(t, 1000)
			token := createReserveTestToken(t, 1000)
			token.UserId = user.Id
			require.NoError(t, db.Model(&token).Update("user_id", user.Id).Error)
			channel := Channel{Type: 1, Key: "unused"}
			require.NoError(t, db.Create(&channel).Error)
			job := BatchJob{PublicID: "batch-test", UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, IdempotencyKey: "once", RequestHash: "hash", Status: "in_progress", BillingStatus: "reserved", SubmitState: "submitted", RequestCount: 2, ReservedQuota: 600}
			items := []BatchItem{{CustomID: "paid", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 300}, {CustomID: "failed", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 300}}
			require.NoError(t, CreateBatchReservation(&job, items))
			require.NoError(t, ConfirmBatchReservation(&job))
			_, err := GetBatchJob(user.Id+1, job.PublicID)
			assert.Error(t, err)
			ok, err := TryReserveUserQuota(user.Id, 401)
			require.NoError(t, err)
			assert.False(t, ok, "synchronous requests must respect batch holds")
			ok, err = TryReserveTokenQuota(token.Id, token.Key, 401, false)
			require.NoError(t, err)
			assert.False(t, ok)
			ok, err = TryReserveUserQuota(user.Id, 400)
			require.NoError(t, err)
			require.True(t, ok)
			ok, err = TryReserveTokenQuota(token.Id, token.Key, 400, false)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, StoreBatchResult(job.ID, "paid", `{"custom_id":"paid","body":{"usage":{"prompt_tokens":5}}}`, "result-hash"))
			require.NoError(t, StoreBatchResult(job.ID, "paid", `{"custom_id":"paid","body":{"usage":{"prompt_tokens":5}}}`, "result-hash"))
			assert.Error(t, StoreBatchResult(job.ID, "paid", `{}`, "different-hash"))
			assert.Error(t, StoreBatchResult(job.ID, "foreign", `{}`, "foreign-hash"))
			// Two workers may observe the same completed item after lease expiry.
			start := make(chan struct{})
			var workers sync.WaitGroup
			errors := make(chan error, 2)
			if dialect == "sqlite" {
				sqlDB, e := db.DB()
				require.NoError(t, e)
				sqlDB.SetMaxOpenConns(1)
			}
			for range 2 {
				workers.Go(func() {
					<-start
					errors <- SettleBatchItem(job.ID, "paid", 200, `{"client_quota":200,"other":{"admin_info":{"quota_saturation":{"kind":"overflow"}}}}`)
				})
			}
			close(start)
			workers.Wait()
			close(errors)
			for err := range errors {
				require.NoError(t, err)
			}
			require.NoError(t, SettleBatchItem(job.ID, "failed", 0, `{"unbilled":true}`))
			assert.ErrorIs(t, SettleBatchItem(job.ID, "paid", 201, `{}`), ErrBatchConflict)
			require.NoError(t, ReleaseSettledBatchHolds(job.ID))
			require.NoError(t, ReleaseSettledBatchHolds(job.ID))
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			require.NoError(t, db.First(&job, job.ID).Error)
			assert.Equal(t, 400, user.Quota)
			assert.Equal(t, 200, user.UsedQuota)
			assert.EqualValues(t, 0, user.BatchHeldQuota)
			assert.Equal(t, 400, token.RemainQuota)
			assert.Equal(t, 600, token.UsedQuota)
			assert.EqualValues(t, 0, token.BatchHeldQuota)
			assert.EqualValues(t, 200, job.ChargedQuota)
			assert.EqualValues(t, 0, job.ReservedQuota)
			assert.Equal(t, 2, job.SettledCount)
			assert.Equal(t, 1, job.ResultCount)
			assert.Equal(t, "settled", job.BillingStatus)
			assert.Equal(t, "reconciliation_required", job.PurchaseStatus, "client settlement must not conceal unknown purchase cost")
			require.NoError(t, SyncBatchConsumeLogs(100))
			require.NoError(t, db.Model(&BatchLogEvent{}).Where("1 = 1").Update("state", "pending").Error)
			require.NoError(t, SyncBatchConsumeLogs(100))
			var consumeCount int64
			require.NoError(t, db.Model(&Log{}).Where("type = ?", LogTypeConsume).Count(&consumeCount).Error)
			assert.EqualValues(t, 2, consumeCount, "outbox replay must not duplicate expense history")
			var paidLog Log
			require.NoError(t, db.Where("quota = ?", 200).First(&paidLog).Error)
			assert.Contains(t, paidLog.Other, "quota_saturation", "financial audit must survive receipt-to-log delivery")

			// Repeat startup/migration against populated tables; receipts and
			// uniqueness must survive both passes.
			for range 2 {
				require.NoError(t, db.AutoMigrate(models...))
			}
			duplicate := BatchJob{PublicID: "other", UserID: user.Id, TokenID: token.Id, IdempotencyKey: "once"}
			assert.Error(t, db.Create(&duplicate).Error)
			require.NoError(t, db.First(&job, job.ID).Error)
			assert.EqualValues(t, 200, job.ChargedQuota)
			// A real cache effect is replayed after a simulated worker crash
			// between Lua success and acknowledging its durable outbox event.
			r := miniredis.RunT(t)
			common.RedisEnabled = true
			common.RDB = redis.NewClient(&redis.Options{Addr: r.Addr()})
			t.Cleanup(func() { _ = common.RDB.Close() })
			job2 := BatchJob{PublicID: "batch-cache", UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, IdempotencyKey: "cache", RequestHash: "hash", RequestCount: 1, ReservedQuota: 100}
			require.NoError(t, CreateBatchReservation(&job2, []BatchItem{{CustomID: "one", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 100}}))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, ConfirmBatchReservation(&job2))
			require.NoError(t, SettleBatchItem(job2.ID, "one", 50, `{"client_quota":50}`))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, db.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND debit_quota > 0", job2.ID).Update("applied", false).Error)
			require.NoError(t, SyncBatchQuotaEvents(100))
			cached, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 350, cached.Quota)
			require.NoError(t, ReleaseSettledBatchHolds(job2.ID))
			require.NoError(t, SyncBatchQuotaEvents(100))
			r.FlushAll()
			cached, err = GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 350, cached.Quota)
			assert.EqualValues(t, 0, cached.BatchHeldQuota)
			// Token revocation cannot strand a submitted batch's wallet hold or debit.
			job3 := BatchJob{PublicID: "batch-revoked", UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, IdempotencyKey: "revoked", RequestCount: 1, ReservedQuota: 80}
			require.NoError(t, CreateBatchReservation(&job3, []BatchItem{{CustomID: "revoked", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 80}}))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.ErrorIs(t, user.HardDelete(), ErrBatchConflict)
			require.NoError(t, token.Delete())
			require.NoError(t, db.Delete(&user).Error)
			require.NoError(t, RaiseBatchItemReservation(job3.ID, "revoked", 90))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, ConfirmBatchReservation(&job3))
			require.NoError(t, SettleBatchItem(job3.ID, "revoked", 60, `{"client_quota":60,"purchase_status":"calculated","purchase_usd":0.001}`))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, ReleaseSettledBatchHolds(job3.ID))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, db.First(&job3, job3.ID).Error)
			assert.Equal(t, "settled", job3.PurchaseStatus)
			require.NoError(t, db.Unscoped().First(&user, user.Id).Error)
			assert.Equal(t, 290, user.Quota)
			assert.Zero(t, user.BatchHeldQuota)
			_, err = GetTokenByKey(token.Key, false)
			assert.Error(t, err, "financial recovery must not restore token authentication")
			// Actual usage can overrun both the original hold and remaining funds.
			debtUser := createReserveTestUser(t, 100)
			debtToken := createReserveTestToken(t, 100)
			require.NoError(t, db.Model(&debtToken).Update("user_id", debtUser.Id).Error)
			debtJob := BatchJob{PublicID: "batch-debt", UserID: debtUser.Id, TokenID: debtToken.Id, ChannelID: channel.Id, IdempotencyKey: "debt", RequestCount: 3, ReservedQuota: 90}
			require.NoError(t, CreateBatchReservation(&debtJob, []BatchItem{
				{CustomID: "overrun", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 30},
				{CustomID: "next", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 30},
				{CustomID: "pending", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 30},
			}))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, ConfirmBatchReservation(&debtJob))
			receipt := `{"client_quota":150,"purchase_status":"calculated"}`
			require.NoError(t, SettleBatchItem(debtJob.ID, "overrun", 150, receipt))
			require.NoError(t, ReleaseSettledBatchHolds(debtJob.ID))
			require.NoError(t, db.First(&debtUser, debtUser.Id).Error)
			assert.Equal(t, -50, debtUser.Quota)
			assert.EqualValues(t, 90, debtUser.BatchHeldQuota, "hold survives until cache acknowledgement")
			require.NoError(t, SyncBatchQuotaEvents(100))
			// Simulate crash after cache application but before durable acknowledgement.
			require.NoError(t, db.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND debit_quota > 0", debtJob.ID).Update("applied", false).Error)
			require.NoError(t, SettleBatchItem(debtJob.ID, "overrun", 150, receipt))
			assert.ErrorIs(t, SettleBatchItem(debtJob.ID, "overrun", 151, receipt), ErrBatchConflict)
			assert.ErrorIs(t, SettleBatchItem(debtJob.ID, "overrun", 150, `{}`), ErrBatchConflict)
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, ReleaseSettledBatchHolds(debtJob.ID))
			require.NoError(t, SyncBatchQuotaEvents(100))
			cachedDebt, err := GetUserCache(debtUser.Id)
			require.NoError(t, err)
			assert.Equal(t, -50, cachedDebt.Quota)
			assert.EqualValues(t, 60, cachedDebt.BatchHeldQuota, "other item holds remain")
			cachedDebtToken, err := GetTokenByKey(debtToken.Key, false)
			require.NoError(t, err)
			assert.Equal(t, -50, cachedDebtToken.RemainQuota)
			require.NoError(t, SettleBatchItem(debtJob.ID, "next", 20, `{"client_quota":20}`))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, ReleaseSettledBatchHolds(debtJob.ID))
			require.NoError(t, SyncBatchQuotaEvents(100))
			require.NoError(t, db.First(&debtUser, debtUser.Id).Error)
			assert.Equal(t, -70, debtUser.Quota)
			assert.EqualValues(t, 30, debtUser.BatchHeldQuota)
			var debtEvents int64
			require.NoError(t, db.Model(&BatchLogEvent{}).Where("item_id IN (?)", db.Model(&BatchItem{}).Select("id").Where("batch_id = ?", debtJob.ID)).Count(&debtEvents).Error)
			assert.EqualValues(t, 2, debtEvents, "one log event per settled item")
			// Reject signed SQL-field and counter overflow before any mutation.
			common.RedisEnabled = false
			for _, boundary := range []struct {
				name    string
				owner   any
				updates map[string]any
			}{
				{"wallet underflow", &debtUser, map[string]any{"quota": math.MinInt32}},
				{"user usage overflow", &debtUser, map[string]any{"used_quota": math.MaxInt32}},
				{"user requests overflow", &debtUser, map[string]any{"request_count": math.MaxInt32}},
				{"token underflow", &debtToken, map[string]any{"remain_quota": math.MinInt}},
				{"token usage overflow", &debtToken, map[string]any{"used_quota": math.MaxInt}},
				{"channel usage overflow", &channel, map[string]any{"used_quota": int64(math.MaxInt64)}},
				{"job charge overflow", &debtJob, map[string]any{"charged_quota": int64(math.MaxInt64)}},
				{"job count overflow", &debtJob, map[string]any{"settled_count": math.MaxInt}},
			} {
				t.Run(boundary.name, func(t *testing.T) {
					require.NoError(t, db.Model(&debtUser).Updates(map[string]any{"quota": -70, "used_quota": 170, "request_count": 2}).Error)
					require.NoError(t, db.Model(&debtToken).Updates(map[string]any{"remain_quota": -70, "used_quota": 170}).Error)
					require.NoError(t, db.Model(&channel).Update("used_quota", 0).Error)
					require.NoError(t, db.Model(&debtJob).Updates(map[string]any{"charged_quota": 170, "settled_count": 2}).Error)
					require.NoError(t, db.Model(boundary.owner).Updates(boundary.updates).Error)
					var beforeUser User
					var beforeToken Token
					var beforeJob BatchJob
					var beforeChannel Channel
					require.NoError(t, db.First(&beforeUser, debtUser.Id).Error)
					require.NoError(t, db.First(&beforeToken, debtToken.Id).Error)
					require.NoError(t, db.First(&beforeJob, debtJob.ID).Error)
					require.NoError(t, db.First(&beforeChannel, channel.Id).Error)
					require.Error(t, SettleBatchItem(debtJob.ID, "pending", 1, `{"client_quota":1}`))
					var afterUser User
					var afterToken Token
					var afterJob BatchJob
					var afterChannel Channel
					require.NoError(t, db.First(&afterUser, debtUser.Id).Error)
					require.NoError(t, db.First(&afterToken, debtToken.Id).Error)
					require.NoError(t, db.First(&afterJob, debtJob.ID).Error)
					require.NoError(t, db.First(&afterChannel, channel.Id).Error)
					assert.Equal(t, beforeUser, afterUser)
					assert.Equal(t, beforeToken, afterToken)
					assert.Equal(t, beforeJob, afterJob)
					assert.Equal(t, beforeChannel, afterChannel)
					var pending BatchItem
					require.NoError(t, db.Where("batch_id = ? AND custom_id = ?", debtJob.ID, "pending").First(&pending).Error)
					assert.Equal(t, "reserved", pending.BillingStatus)
				})
			}
			// Upgrade an actual v0.3.4 model schema, with existing wallet/token values.
			common.RedisEnabled = false
			for i := len(models) - 1; i >= 0; i-- {
				require.NoError(t, db.Migrator().DropTable(models[i]))
			}
			require.NoError(t, db.AutoMigrate(&batchReleasedUserV034{}, &batchReleasedTokenV034{}))
			legacyUser := batchReleasedUserV034{Username: "released-user", Password: "unused", Quota: 567, UsedQuota: 89, AuthVersion: 7}
			require.NoError(t, db.Create(&legacyUser).Error)
			legacyToken := batchReleasedTokenV034{UserId: legacyUser.Id, Key: "released-key", RemainQuota: 456, UsedQuota: 78}
			require.NoError(t, db.Create(&legacyToken).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(models...))
			}
			var upgradedUser User
			var upgradedToken Token
			require.NoError(t, db.First(&upgradedUser, legacyUser.Id).Error)
			require.NoError(t, db.First(&upgradedToken, legacyToken.Id).Error)
			assert.Equal(t, 567, upgradedUser.Quota)
			assert.Equal(t, 89, upgradedUser.UsedQuota)
			assert.EqualValues(t, 7, upgradedUser.AuthVersion)
			assert.Zero(t, upgradedUser.BatchHeldQuota)
			assert.Zero(t, upgradedUser.BatchQuotaVersion)
			assert.Equal(t, 456, upgradedToken.RemainQuota)
			assert.Equal(t, 78, upgradedToken.UsedQuota)
			assert.Zero(t, upgradedToken.BatchHeldQuota)
			assert.Zero(t, upgradedToken.BatchQuotaVersion)
			assert.Error(t, db.Create(&Token{UserId: legacyUser.Id, Key: legacyToken.Key}).Error, "token uniqueness survives upgrade")

		})
	}
}

// Released v0.3.4 schema, retained independently of the current ORM model.
type batchReleasedUserV034 struct {
	Id                   int                        `json:"id"`
	Username             string                     `json:"username" gorm:"unique;index" validate:"max=20"`
	Password             string                     `json:"password" gorm:"not null;" validate:"min=8,max=128"`
	HasPassword          bool                       `json:"-" gorm:"-:all"`
	OriginalPassword     string                     `json:"original_password" gorm:"-:all"` // this field is only for Password change verification, don't save it to database!
	DisplayName          string                     `json:"display_name" gorm:"index" validate:"max=20"`
	Role                 int                        `json:"role" gorm:"type:int;default:1"`   // admin, common
	Status               int                        `json:"status" gorm:"type:int;default:1"` // enabled, disabled
	Email                string                     `json:"email" gorm:"index" validate:"max=50"`
	GitHubId             string                     `json:"github_id" gorm:"column:github_id;index"`
	DiscordId            string                     `json:"discord_id" gorm:"column:discord_id;index"`
	OidcId               string                     `json:"oidc_id" gorm:"column:oidc_id;index"`
	WeChatId             string                     `json:"wechat_id" gorm:"column:wechat_id;index"`
	TelegramId           string                     `json:"telegram_id" gorm:"column:telegram_id;index"`
	VerificationCode     string                     `json:"verification_code" gorm:"-:all"`                         // this field is only for Email verification, don't save it to database!
	AccessToken          *string                    `json:"-" gorm:"type:char(32);column:access_token;uniqueIndex"` // this token is for system management
	AccessTokenCreatedAt *int64                     `json:"-" gorm:"type:bigint;column:access_token_created_at"`
	Quota                int                        `json:"quota" gorm:"type:int;default:0"`
	UsedQuota            int                        `json:"used_quota" gorm:"type:int;default:0;column:used_quota"` // used quota
	RequestCount         int                        `json:"request_count" gorm:"type:int;default:0;"`               // request number
	Group                string                     `json:"group" gorm:"type:varchar(64);default:'default'"`
	AffCode              string                     `json:"aff_code" gorm:"type:varchar(32);column:aff_code;uniqueIndex"`
	AffCount             int                        `json:"aff_count" gorm:"type:int;default:0;column:aff_count"`
	AffQuota             int                        `json:"aff_quota" gorm:"type:int;default:0;column:aff_quota"`           // 邀请剩余额度
	AffHistoryQuota      int                        `json:"aff_history_quota" gorm:"type:int;default:0;column:aff_history"` // 邀请历史额度
	InviterId            int                        `json:"inviter_id" gorm:"type:int;column:inviter_id;index"`
	DeletedAt            gorm.DeletedAt             `gorm:"index"`
	LinuxDOId            string                     `json:"linux_do_id" gorm:"column:linux_do_id;index"`
	Setting              string                     `json:"setting" gorm:"type:text;column:setting"`
	Remark               string                     `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	StripeCustomer       string                     `json:"stripe_customer" gorm:"type:varchar(64);column:stripe_customer;index"`
	CreatedAt            int64                      `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	LastLoginAt          int64                      `json:"last_login_at" gorm:"default:0;column:last_login_at"`
	AuthVersion          int64                      `json:"-" gorm:"type:bigint;not null;default:1;column:auth_version"`
	AdminPermissions     map[string]map[string]bool `json:"admin_permissions,omitempty" gorm:"-:all"`
}

func (batchReleasedUserV034) TableName() string { return "batch_contract_users" }

// Released v0.3.4 schema, retained independently of the current ORM model.
type batchReleasedTokenV034 struct {
	Id                 int            `json:"id"`
	UserId             int            `json:"user_id" gorm:"index"`
	Key                string         `json:"key" gorm:"type:varchar(128);uniqueIndex"`
	Status             int            `json:"status" gorm:"default:1"`
	Name               string         `json:"name" gorm:"index" `
	CreatedTime        int64          `json:"created_time" gorm:"bigint"`
	AccessedTime       int64          `json:"accessed_time" gorm:"bigint"`
	ExpiredTime        int64          `json:"expired_time" gorm:"bigint;default:-1"` // -1 means never expired
	RemainQuota        int            `json:"remain_quota" gorm:"default:0"`
	UnlimitedQuota     bool           `json:"unlimited_quota"`
	ModelLimitsEnabled bool           `json:"model_limits_enabled"`
	ModelLimits        string         `json:"model_limits" gorm:"type:text"`
	AllowIps           *string        `json:"allow_ips" gorm:"default:''"`
	UsedQuota          int            `json:"used_quota" gorm:"default:0"` // used quota
	Group              string         `json:"group" gorm:"default:''"`
	CrossGroupRetry    bool           `json:"cross_group_retry"` // 跨分组重试，仅auto分组有效
	AutoGroups         string         `json:"-" gorm:"type:text"`
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (batchReleasedTokenV034) TableName() string { return "batch_contract_tokens" }

func TestBatchSeparateLogDatabase(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres", "clickhouse"} {
		t.Run(dialect, func(t *testing.T) {
			primary, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "primary.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, primary.AutoMigrate(&BatchLogEvent{}))
			var logDriver gorm.Dialector
			kind := common.DatabaseTypeSQLite
			switch dialect {
			case "mysql":
				dsn := os.Getenv("TEST_LOG_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_LOG_MYSQL_DSN is not configured")
				}
				logDriver = mysql.Open(dsn + "&clientFoundRows=true")
				kind = common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("TEST_LOG_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_LOG_POSTGRES_DSN is not configured")
				}
				logDriver = postgres.Open(dsn)
				kind = common.DatabaseTypePostgreSQL
			case "clickhouse":
				dsn := os.Getenv("TEST_LOG_CLICKHOUSE_DSN")
				if dsn == "" {
					t.Skip("TEST_LOG_CLICKHOUSE_DSN is not configured")
				}
				logDriver = clickhouse.Open(dsn)
				kind = common.DatabaseTypeClickHouse
			default:
				logDriver = sqlite.Open(filepath.Join(t.TempDir(), "logs.db"))
			}
			logs, err := gorm.Open(logDriver, &gorm.Config{})
			require.NoError(t, err)
			oldDB, oldLogDB, oldKind := DB, LOG_DB, common.LogDatabaseType()
			DB, LOG_DB = primary, logs
			common.SetLogDatabaseType(kind)
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLogDB
				common.SetLogDatabaseType(oldKind)
				for _, db := range []*gorm.DB{primary, logs} {
					sqlDB, e := db.DB()
					if e == nil {
						_ = sqlDB.Close()
					}
				}
			})
			if dialect == "clickhouse" {
				require.NoError(t, logs.Exec(clickHouseLogCreateTableSQL(0)).Error)
				require.NoError(t, logs.Exec("TRUNCATE TABLE logs").Error)
			} else {
				logs.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_separate_log_"}
				require.NoError(t, logs.AutoMigrate(&Log{}, &BatchLogReceipt{}))
				t.Cleanup(func() { _ = logs.Migrator().DropTable(&BatchLogReceipt{}, &Log{}) })
				for range 2 {
					require.NoError(t, logs.AutoMigrate(&Log{}, &BatchLogReceipt{}))
				}
			}
			for _, db := range []*gorm.DB{primary, logs} {
				sqlDB, e := db.DB()
				require.NoError(t, e)
				sqlDB.SetMaxOpenConns(1)
			}
			payload, err := common.Marshal(Log{UserId: 1, Type: LogTypeConsume, RequestId: "batch-log-contract", Quota: 10, CreatedAt: common.GetTimestamp(), Other: `{"admin_info":{"quota_saturation":{"kind":"overflow"}}}`})
			require.NoError(t, err)
			event := BatchLogEvent{ItemID: 1, State: "pending", Payload: string(payload)}
			require.NoError(t, primary.Create(&event).Error)
			require.NoError(t, SyncBatchConsumeLogs(10))
			state := "pending"
			if dialect == "clickhouse" {
				state = "writing"
			}
			require.NoError(t, primary.Model(&event).Update("state", state).Error)
			require.NoError(t, SyncBatchConsumeLogs(10))
			require.NoError(t, SyncBatchConsumeLogs(10))
			var count int64
			require.NoError(t, logs.Model(&Log{}).Where("request_id = ?", "batch-log-contract").Count(&count).Error)
			assert.EqualValues(t, 1, count, "lost acknowledgement must not duplicate log or expense statistics")
			if dialect == "clickhouse" {
				payload, err = common.Marshal(Log{RequestId: "ambiguous-before-insert", CreatedAt: common.GetTimestamp()})
				require.NoError(t, err)
				require.NoError(t, primary.Create(&BatchLogEvent{ItemID: 2, State: "writing", Payload: string(payload)}).Error)
				require.Error(t, SyncBatchConsumeLogs(10))
				require.NoError(t, logs.Model(&Log{}).Where("request_id = ?", "ambiguous-before-insert").Count(&count).Error)
				assert.Zero(t, count, "unknown insert must not be resent")
			}
		})
	}
}

func TestBatchModelPricing(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openModelCostFXTestDB(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_pricing_"}
			previousDB, previousKeyCol := DB, commonKeyCol
			previousOptions := common.OptionMap
			previousConfigs := config.GlobalConfig.ExportAllConfigs()
			previousRatios := map[string]string{
				"ModelPrice":           ratio_setting.ModelPrice2JSONString(),
				"ModelRatio":           ratio_setting.ModelRatio2JSONString(),
				"CompletionRatio":      ratio_setting.CompletionRatio2JSONString(),
				"CacheRatio":           ratio_setting.CacheRatio2JSONString(),
				"CreateCacheRatio":     ratio_setting.CreateCacheRatio2JSONString(),
				"ImageRatio":           ratio_setting.ImageRatio2JSONString(),
				"AudioRatio":           ratio_setting.AudioRatio2JSONString(),
				"AudioCompletionRatio": ratio_setting.AudioCompletionRatio2JSONString(),
			}
			DB = db
			commonKeyCol = "`key`"
			if dialect == "postgres" {
				commonKeyCol = `"key"`
			}
			common.OptionMap = make(map[string]string)
			t.Cleanup(func() {
				for key, value := range previousConfigs {
					require.NoError(t, updateOptionMap(key, value))
				}
				for key, value := range previousRatios {
					require.NoError(t, updateOptionMap(key, value))
				}
				common.OptionMap = previousOptions
				DB, commonKeyCol = previousDB, previousKeyCol
				_ = db.Migrator().DropTable(&Option{})
			})
			require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Model{}, &Vendor{}))
			t.Cleanup(func() { _ = db.Migrator().DropTable(&Vendor{}, &Model{}, &Channel{}) })
			for _, table := range []struct {
				name  string
				value any
			}{{"channels", &Channel{}}, {"abilities", &Ability{}}} {
				if !db.Migrator().HasTable(table.name) {
					require.NoError(t, db.Table(table.name).AutoMigrate(table.value))
					t.Cleanup(func() { _ = db.Migrator().DropTable(table.name) })
				}
			}
			snapshot, err := GetModelPricingSnapshot([]string{"batch-priced-model"})
			require.NoError(t, err)
			require.Len(t, snapshot.Entries, 1)
			values := PricingValues{"ModelRatio": float64(1), "billing_setting.batch_billing_expr": "p * 0.5 + c * 1"}
			change := ModelPricingChange{ModelName: "batch-priced-model", ExpectedVersion: snapshot.Entries[0].Version, Pricing: values}
			require.NoError(t, UpdateModelPricing([]ModelPricingChange{change}))
			saved, err := GetModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, values, saved.Entries[0].Configured)
			assert.ErrorIs(t, UpdateModelPricing([]ModelPricingChange{change}), ErrModelPricingConflict)
			for _, invalid := range []string{"p *", `p * header("x-rate")`} {
				assert.Error(t, UpdateModelPricing([]ModelPricingChange{{ModelName: change.ModelName, ExpectedVersion: saved.Entries[0].Version, Pricing: PricingValues{"billing_setting.batch_billing_expr": invalid}}}))
			}
			unchanged, err := GetModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, saved.Entries[0].Version, unchanged.Entries[0].Version)
			require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: change.ModelName, ExpectedVersion: saved.Entries[0].Version, Pricing: PricingValues{"ModelRatio": float64(1)}}}))
			cleared, err := GetModelPricingSnapshot([]string{change.ModelName})
			require.NoError(t, err)
			assert.Equal(t, PricingValues{"ModelRatio": float64(1)}, cleared.Entries[0].Configured)
		})
	}
}
