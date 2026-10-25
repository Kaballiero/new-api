package batch_test

import (
	billingmath "github.com/QuantumNous/new-api/internal/adapter/billing"

	"context"
	"errors"
	"fmt"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/batch"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	rediscache "github.com/QuantumNous/new-api/internal/adapter/redis/batch"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchfixture"
	. "github.com/QuantumNous/new-api/model"
	model "github.com/QuantumNous/new-api/model"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
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
			db := fixture.OpenDialect(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_contract_"}
			previousDB, previousRedis, previousRDB, previousBatch := DB, common.RedisEnabled, common.RDB, common.BatchUpdateEnabled
			DB = db
			previousLogDB := LOG_DB
			LOG_DB = db
			t.Cleanup(func() { LOG_DB = previousLogDB })
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
			require.NoError(t, reserve(&job, items))
			require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)))
			_, err := testStore().GetBatchJob(user.Id+1, job.PublicID)
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
			require.NoError(t, testStore().StoreBatchResult(job.ID, "paid", `{"custom_id":"paid","body":{"usage":{"prompt_tokens":5}}}`, "result-hash"))
			require.NoError(t, testStore().StoreBatchResult(job.ID, "paid", `{"custom_id":"paid","body":{"usage":{"prompt_tokens":5}}}`, "result-hash"))
			assert.Error(t, testStore().StoreBatchResult(job.ID, "paid", `{}`, "different-hash"))
			assert.Error(t, testStore().StoreBatchResult(job.ID, "foreign", `{}`, "foreign-hash"))
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
					errors <- testStore().SettleBatchItem(job.ID, "paid", 200, `{"client_quota":200,"other":{"admin_info":{"quota_saturation":{"kind":"overflow"}}}}`)
				})
			}
			close(start)
			workers.Wait()
			close(errors)
			for err := range errors {
				require.NoError(t, err)
			}
			require.NoError(t, testStore().SettleBatchItem(job.ID, "failed", 0, `{"unbilled":true}`))
			assert.ErrorIs(t, testStore().SettleBatchItem(job.ID, "paid", 201, `{}`), domain.ErrBatchConflict)
			require.NoError(t, testStore().ReleaseSettledBatchHolds(job.ID))
			require.NoError(t, testStore().ReleaseSettledBatchHolds(job.ID))
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
			require.NoError(t, testStore().SyncBatchConsumeLogs(100))
			require.NoError(t, db.Model(&BatchLogEvent{}).Where("1 = 1").Update("state", "pending").Error)
			require.NoError(t, testStore().SyncBatchConsumeLogs(100))
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
			require.NoError(t, reserve(&job2, []BatchItem{{CustomID: "one", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 100}}))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job2)))
			require.NoError(t, testStore().SettleBatchItem(job2.ID, "one", 50, `{"client_quota":50}`))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, db.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND debit_quota > 0", job2.ID).Update("applied", false).Error)
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			cached, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 350, cached.Quota)
			require.NoError(t, testStore().ReleaseSettledBatchHolds(job2.ID))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			r.FlushAll()
			cached, err = GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 350, cached.Quota)
			assert.EqualValues(t, 0, cached.BatchHeldQuota)
			// Token revocation cannot strand a submitted batch's wallet hold or debit.
			job3 := BatchJob{PublicID: "batch-revoked", UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, IdempotencyKey: "revoked", RequestCount: 1, ReservedQuota: 90}
			require.NoError(t, reserve(&job3, []BatchItem{{CustomID: "revoked", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 90}}))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.ErrorIs(t, user.HardDelete(), model.ErrBatchConflict)
			require.NoError(t, token.Delete())
			require.NoError(t, db.Delete(&user).Error)
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job3)))
			require.NoError(t, testStore().SettleBatchItem(job3.ID, "revoked", 60, `{"client_quota":60,"purchase_status":"calculated","purchase_usd":0.001}`))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, testStore().ReleaseSettledBatchHolds(job3.ID))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
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
			require.NoError(t, reserve(&debtJob, []BatchItem{
				{CustomID: "overrun", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 30},
				{CustomID: "next", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 30},
				{CustomID: "pending", Body: `{}`, BillingStatus: "reserved", ReservedQuota: 30},
			}))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&debtJob)))
			receipt := `{"client_quota":150,"purchase_status":"calculated"}`
			require.NoError(t, testStore().SettleBatchItem(debtJob.ID, "overrun", 150, receipt))
			require.NoError(t, testStore().ReleaseSettledBatchHolds(debtJob.ID))
			require.NoError(t, db.First(&debtUser, debtUser.Id).Error)
			assert.Equal(t, -50, debtUser.Quota)
			assert.EqualValues(t, 90, debtUser.BatchHeldQuota, "hold survives until cache acknowledgement")
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			// Simulate crash after cache application but before durable acknowledgement.
			require.NoError(t, db.Model(&BatchQuotaEvent{}).Where("batch_id = ? AND debit_quota > 0", debtJob.ID).Update("applied", false).Error)
			require.NoError(t, testStore().SettleBatchItem(debtJob.ID, "overrun", 150, receipt))
			assert.ErrorIs(t, testStore().SettleBatchItem(debtJob.ID, "overrun", 151, receipt), domain.ErrBatchConflict)
			assert.ErrorIs(t, testStore().SettleBatchItem(debtJob.ID, "overrun", 150, `{}`), domain.ErrBatchConflict)
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, testStore().ReleaseSettledBatchHolds(debtJob.ID))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			cachedDebt, err := GetUserCache(debtUser.Id)
			require.NoError(t, err)
			assert.Equal(t, -50, cachedDebt.Quota)
			assert.EqualValues(t, 60, cachedDebt.BatchHeldQuota, "other item holds remain")
			cachedDebtToken, err := GetTokenByKey(debtToken.Key, false)
			require.NoError(t, err)
			assert.Equal(t, -50, cachedDebtToken.RemainQuota)
			require.NoError(t, testStore().SettleBatchItem(debtJob.ID, "next", 20, `{"client_quota":20}`))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
			require.NoError(t, testStore().ReleaseSettledBatchHolds(debtJob.ID))
			require.NoError(t, testStore().SyncBatchQuotaEvents(100))
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
					require.Error(t, testStore().SettleBatchItem(debtJob.ID, "pending", 1, `{"client_quota":1}`))
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
			upgradeReads := db
			if dialect == "postgres" {
				// Startup follows schema migration in a new process. Reusing
				// this fixture's pre-upgrade pgx connection would retain cached
				// SELECT * plans for the deliberately replaced released schema.
				upgradeReads = fixture.ReopenPostgres(t, db)
			}
			var upgradedUser User
			var upgradedToken Token
			require.NoError(t, upgradeReads.First(&upgradedUser, legacyUser.Id).Error)
			require.NoError(t, upgradeReads.First(&upgradedToken, legacyToken.Id).Error)
			assert.Equal(t, 567, upgradedUser.Quota)
			assert.Equal(t, 89, upgradedUser.UsedQuota)
			assert.EqualValues(t, 7, upgradedUser.AuthVersion)
			assert.Zero(t, upgradedUser.BatchHeldQuota)
			assert.Zero(t, upgradedUser.BatchQuotaVersion)
			assert.Equal(t, 456, upgradedToken.RemainQuota)
			assert.Equal(t, 78, upgradedToken.UsedQuota)
			assert.Zero(t, upgradedToken.BatchHeldQuota)
			assert.Zero(t, upgradedToken.BatchQuotaVersion)
			assert.Error(t, upgradeReads.Create(&Token{UserId: legacyUser.Id, Key: legacyToken.Key}).Error, "token uniqueness survives upgrade")

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
	AffQuota             int                        `json:"aff_quota" gorm:"type:int;default:0;column:aff_quota"`           // Remaining referral quota
	AffHistoryQuota      int                        `json:"aff_history_quota" gorm:"type:int;default:0;column:aff_history"` // Historical referral quota
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
	CrossGroupRetry    bool           `json:"cross_group_retry"` // Retry across groups; applies only to the auto group
	AutoGroups         string         `json:"-" gorm:"type:text"`
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (batchReleasedTokenV034) TableName() string { return "batch_contract_tokens" }

func TestBatchAggregateCostAccounting(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := fixture.OpenDialect(t, dialect)
			if dialect == "sqlite" {
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(1)
			}
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_aggregate_"}
			previousDB, previousLogDB, previousRedis, previousConsume := DB, LOG_DB, common.RedisEnabled, common.LogConsumeEnabled
			DB, LOG_DB, common.RedisEnabled, common.LogConsumeEnabled = db, db, false, true
			t.Cleanup(func() {
				DB, LOG_DB, common.RedisEnabled, common.LogConsumeEnabled = previousDB, previousLogDB, previousRedis, previousConsume
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
			channel := Channel{Type: 20, Key: "unused"}
			require.NoError(t, db.Create(&channel).Error)
			basis := common.OpenRouterCostBasis{Version: domain.BatchSettlementOpenRouterJobCostV2, EffectiveGroupRatio: 1.45, QuotaPerUnit: 500000}
			encoded, err := common.Marshal(map[string]any{"settlement_mode": domain.BatchSettlementOpenRouterJobCostV2, "openrouter_cost": basis})
			require.NoError(t, err)
			for i, tc := range []struct {
				name, status, usage string
				want                int
				invalid             bool
			}{
				{"aggregate only", "completed", `{"cost":0.0000017,"prompt_tokens":3,"completion_tokens":1}`, 1, false},
				{"mixed and missing outputs", "failed", `{"cost":0.000003}`, 2, false},
				{"explicit zero", "cancelled", `{"cost":0}`, 0, false},
				{"null", "completed", `{"cost":null}`, 0, true},
				{"missing", "completed", `{}`, 0, true},
				{"negative", "completed", `{"cost":-1}`, 0, true},
				{"string", "completed", `{"cost":"0.1"}`, 0, true},
				{"clamp", "completed", `{"cost":1e20}`, 0, true},
				{"nonterminal", "in_progress", `{"cost":0.000003}`, 0, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					job := BatchJob{PublicID: fmt.Sprintf("aggregate-%d", i), UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, IdempotencyKey: fmt.Sprint(i), RequestHash: "hash", Status: tc.status, SubmitState: "submitted", UpstreamID: fmt.Sprintf("native-%d", i), BillingSnapshot: string(encoded), UpstreamUsage: tc.usage, BillingStatus: "reserved", RequestCount: 2, ReservedQuota: 10}
					require.NoError(t, reserve(&job, []BatchItem{{CustomID: "one", Body: `{}`, ReservedQuota: 5, BillingStatus: "reserved"}, {CustomID: "two", Body: `{}`, ReservedQuota: 5, BillingStatus: "reserved"}}))
					var before User
					require.NoError(t, db.First(&before, user.Id).Error)
					var err error
					if tc.name == "aggregate only" {
						start := make(chan struct{})
						outcomes := make(chan error, 2)
						for range 2 {
							go func() { <-start; outcomes <- testStore().SettleBatchJobCost(job.ID) }()
						}
						close(start)
						require.NoError(t, <-outcomes)
						err = <-outcomes
					} else {
						err = testStore().SettleBatchJobCost(job.ID)
					}
					if tc.invalid {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					var after User
					require.NoError(t, db.First(&after, user.Id).Error)
					if tc.invalid || tc.status == "in_progress" {
						assert.Equal(t, before.Quota, after.Quota)
						require.NoError(t, db.First(&job, job.ID).Error)
						assert.Empty(t, job.FinancialDetails)
						if tc.invalid {
							assert.Equal(t, "reconciliation_required", job.BillingStatus)
						}
						assert.EqualValues(t, 10, job.ReservedQuota)
						return
					}
					if tc.name == "aggregate only" {
						transient := errors.New("injected transient database failure")
						require.NoError(t, db.Callback().Query().Before("gorm:query").Register("aggregate_transient", func(query *gorm.DB) {
							if query.Statement.Table == "batch_aggregate_users" {
								query.AddError(transient)
							}
						}))
						retryErr := testStore().SettleBatchJobCost(job.ID)
						require.NoError(t, db.Callback().Query().Remove("aggregate_transient"))
						require.ErrorIs(t, retryErr, transient)
						require.NoError(t, db.First(&job, job.ID).Error)
						assert.Equal(t, "pending_cache", job.BillingStatus, "transient failure cannot poison a committed receipt")
					}
					require.NoError(t, testStore().SettleBatchJobCost(job.ID))
					require.ErrorIs(t, testStore().SettleBatchItem(job.ID, "one", 1, `{"purchase_status":"calculated"}`), domain.ErrBatchConflict)
					assert.Equal(t, before.Quota-tc.want, after.Quota)
					assert.Equal(t, before.RequestCount+2, after.RequestCount)
					var items []BatchItem
					require.NoError(t, db.Where("batch_id = ?", job.ID).Find(&items).Error)
					for _, item := range items {
						assert.Zero(t, item.ChargedQuota)
						assert.Empty(t, item.FinancialDetails)
						assert.Equal(t, "settled_at_batch", item.BillingStatus)
					}
					var events int64
					require.NoError(t, db.Model(&BatchLogEvent{}).Where("batch_id = ? AND item_id IS NULL", job.ID).Count(&events).Error)
					assert.EqualValues(t, 1, events)
					// Pending durable cache effects must keep the reservation.
					require.NoError(t, db.Model(&BatchQuotaEvent{}).Where("batch_id = ?", job.ID).Update("applied", false).Error)
					require.NoError(t, testStore().ReleaseSettledBatchHolds(job.ID))
					require.NoError(t, db.First(&job, job.ID).Error)
					assert.EqualValues(t, 10, job.ReservedQuota)
					require.NoError(t, testStore().SyncBatchQuotaEvents(1000))
					releaseStart := make(chan struct{})
					releaseOutcomes := make(chan error, 2)
					for range 2 {
						go func() { <-releaseStart; releaseOutcomes <- testStore().ReleaseSettledBatchHolds(job.ID) }()
					}
					close(releaseStart)
					require.NoError(t, <-releaseOutcomes)
					require.NoError(t, <-releaseOutcomes)
					require.NoError(t, testStore().SyncBatchConsumeLogs(1000))
					require.NoError(t, testStore().SyncBatchConsumeLogs(1000))
					require.NoError(t, db.First(&job, job.ID).Error)
					if tc.name == "aggregate only" {
						var aggregateLog Log
						require.NoError(t, db.Where("content = ?", "Batch aggregate").Order("id asc").First(&aggregateLog).Error)
						assert.Equal(t, 3, aggregateLog.PromptTokens)
						assert.Equal(t, 1, aggregateLog.CompletionTokens)
					}
					assert.Equal(t, "settled", job.BillingStatus)
					assert.Zero(t, job.ReservedQuota)
					// A provider correction cannot rewrite the original debit.
					require.NoError(t, db.Model(&job).Update("upstream_usage", `{"cost":1}`).Error)
					require.ErrorIs(t, testStore().SettleBatchJobCost(job.ID), domain.ErrBatchConflict)
					require.NoError(t, db.First(&job, job.ID).Error)
					assert.Equal(t, "reconciliation_required", job.BillingStatus)

					var final User
					require.NoError(t, db.First(&final, user.Id).Error)
					assert.Equal(t, after.Quota, final.Quota)
				})
			}
		})
	}
}

func TestBatchDispatchReservation(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := fixture.OpenDialect(t, dialect)
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "batch_dispatch_"}
			oldDB, oldRedis, oldRDB := DB, common.RedisEnabled, common.RDB
			DB, common.RedisEnabled = db, true
			r := miniredis.RunT(t)
			addr := r.Addr()
			if configured := os.Getenv("TEST_BATCH_REDIS_ADDR"); configured != "" {
				addr = configured // Dedicated disposable instance supplied by the fixture runner.
			}
			client := redis.NewClient(&redis.Options{Addr: addr})
			common.RDB = client
			t.Cleanup(func() { _ = client.Close(); DB, common.RedisEnabled, common.RDB = oldDB, oldRedis, oldRDB })
			models := []any{&User{}, &Token{}, &BatchJob{}, &BatchItem{}, &BatchQuotaEvent{}}
			for range 2 {
				require.NoError(t, db.AutoMigrate(models...))
			}
			t.Cleanup(func() {
				for i := len(models) - 1; i >= 0; i-- {
					_ = db.Migrator().DropTable(models[i])
				}
			})
			for _, mode := range []string{"cold", "ttl_expired", "evicted_after_confirmation", "fence", "disabled", "expired", "deleted", "ownership", "missing_hold", "cache_hold", "revoked_missing_hold", "revoked_cache_hold", "cache_version", "other_batch_pending", "live_quota", "expired_lease", "concurrent_markers", "redis_off"} {
				t.Run(mode, func(t *testing.T) {
					user := createReserveTestUser(t, 1000)
					token := createReserveTestToken(t, 1000)
					require.NoError(t, db.Model(&token).Update("user_id", user.Id).Error)
					job := BatchJob{PublicID: "dispatch-" + mode, UserID: user.Id, TokenID: token.Id, IdempotencyKey: mode, RequestCount: 1, ReservedQuota: 100, SubmitState: "preparing", LeaseOwner: "worker", LeaseUntil: common.GetTimestamp() + 120}
					require.NoError(t, reserve(&job, []BatchItem{{CustomID: "one", Body: `{}`, ReservedQuota: 100}}))
					require.NoError(t, testStore().SyncBatchQuotaEvents(1000))
					require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)))
					var expected error
					switch mode {
					case "cold":
						require.NoError(t, client.Del(context.Background(), GetTokenCacheKey(token.Key)).Err())
						require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)))
					case "ttl_expired":
						require.NoError(t, client.Expire(context.Background(), GetTokenCacheKey(token.Key), time.Second).Err())
						if addr == r.Addr() {
							r.FastForward(2 * time.Second)
						} else {
							t.Skip("deterministic TTL clock is provided by miniredis")
						}
						require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)))
					case "evicted_after_confirmation":
						require.NoError(t, client.Del(context.Background(), GetTokenCacheKey(token.Key)).Err())
						require.ErrorIs(t, testStore().MarkBatchSubmissionUnknown((*domain.Job)(&job), "worker"), domain.ErrBatchReservationPending)
						require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)))
					case "fence":
						require.NoError(t, token.Update())
						require.ErrorIs(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)), domain.ErrBatchReservationPending, "DB fallback under a fence is not permission")
						require.ErrorIs(t, testStore().MarkBatchSubmissionUnknown((*domain.Job)(&job), "worker"), domain.ErrBatchReservationPending)
						if addr == r.Addr() {
							r.FastForward(11 * time.Second)
						} else {
							require.NoError(t, client.Del(context.Background(), GetTokenCacheFenceKey(token.Key)).Err())
						}
						require.NoError(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)), "a new pass hydrates after fence expiry")
					case "disabled":
						// DB revoke commits after preflight; deliberately leave stale enabled Redis.
						require.NoError(t, db.Model(&token).Update("status", common.TokenStatusDisabled).Error)
						expected = domain.ErrBatchDispatchDenied
					case "expired":
						require.NoError(t, db.Model(&token).Update("expired_time", common.GetTimestamp()-1).Error)
						expected = domain.ErrBatchDispatchDenied
					case "deleted":
						require.NoError(t, db.Delete(&token).Error)
						expected = domain.ErrBatchDispatchDenied
					case "ownership":
						require.NoError(t, db.Model(&token).Update("user_id", user.Id+1).Error)
						expected = domain.ErrBatchReservationUnsafe
					case "missing_hold":
						require.NoError(t, db.Model(&token).Update("batch_held_quota", 0).Error)
						expected = domain.ErrBatchReservationUnsafe
					case "cache_hold":
						require.NoError(t, client.HSet(context.Background(), GetTokenCacheKey(token.Key), "BatchHeldQuota", 0).Err())
						expected = domain.ErrBatchReservationUnsafe
					case "revoked_missing_hold", "revoked_cache_hold":
						require.NoError(t, db.Model(&token).Update("status", common.TokenStatusDisabled).Error)
						if mode == "revoked_missing_hold" {
							require.NoError(t, db.Model(&token).Update("batch_held_quota", 50).Error)
						} else {
							require.NoError(t, client.HSet(context.Background(), GetTokenCacheKey(token.Key), "BatchHeldQuota", 0).Err())
						}
						require.ErrorIs(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)), domain.ErrBatchReservationUnsafe)
						expected = domain.ErrBatchReservationUnsafe
					case "cache_version":
						require.NoError(t, client.HSet(context.Background(), GetTokenCacheKey(token.Key), "BatchQuotaVersion", 0).Err())
						expected = domain.ErrBatchReservationPending
					case "other_batch_pending":
						other := BatchJob{PublicID: "dispatch-other", UserID: user.Id, TokenID: token.Id, IdempotencyKey: "other", RequestCount: 1, ReservedQuota: 50}
						require.NoError(t, reserve(&other, []BatchItem{{CustomID: "two", Body: `{}`, ReservedQuota: 50}}))
						expected = domain.ErrBatchReservationPending
					case "live_quota":
						require.NoError(t, client.HSet(context.Background(), GetTokenCacheKey(token.Key), "RemainQuota", 99).Err())
						require.ErrorIs(t, testStore().ConfirmBatchReservation((*domain.Job)(&job)), domain.ErrBatchInsufficientQuota)
						remain, err := client.HGet(context.Background(), GetTokenCacheKey(token.Key), "RemainQuota").Int()
						require.NoError(t, err)
						assert.Equal(t, 99, remain, "hydration must not replace live quota with DB1000")
						expected = domain.ErrBatchInsufficientQuota
					case "expired_lease":
						require.NoError(t, db.Model(&job).Update("lease_until", common.GetTimestamp()-1).Error)
						expected = domain.ErrBatchConflict
					case "concurrent_markers":
						start, outcomes := make(chan struct{}), make(chan error, 2)
						for range 2 {
							go func() { <-start; outcomes <- testStore().MarkBatchSubmissionUnknown((*domain.Job)(&job), "worker") }()
						}
						close(start)
						successes := 0
						for range 2 {
							if err := <-outcomes; err == nil {
								successes++
							}
						}
						assert.Equal(t, 1, successes, "only one transaction may authorize the provider POST")
						expected = domain.ErrBatchConflict
					case "redis_off":
						common.RedisEnabled = false
						defer func() { common.RedisEnabled = true }()
					}
					err := testStore().MarkBatchSubmissionUnknown((*domain.Job)(&job), "worker")
					if expected == nil {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, expected)
					}
					var stored BatchJob
					require.NoError(t, db.First(&stored, job.ID).Error)
					if expected == nil || mode == "concurrent_markers" {
						assert.Equal(t, "submission_unknown", stored.SubmitState)
					} else {
						assert.Equal(t, "preparing", stored.SubmitState)
					}
					assert.EqualValues(t, 100, stored.ReservedQuota)
					assert.Zero(t, stored.ChargedQuota)
				})
			}
		})
	}
}
func createReserveTestUser(t *testing.T, quota int) User {
	t.Helper()
	user := User{
		Username:    "reserve-user-" + common.GetRandomString(6),
		Password:    "unused-password-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		Quota:       quota,
		AffCode:     "reserve-aff-" + common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createReserveTestToken(t *testing.T, remainQuota int) Token {
	t.Helper()
	token := Token{
		UserId:      1,
		Key:         "reserve-token-" + common.GetRandomString(8),
		Name:        "reserve-test",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: remainQuota,
	}
	require.NoError(t, token.Insert())
	return token
}

func testStore() storage.Store {
	return storage.NewStore(func() *gorm.DB { return model.DB }, func() *gorm.DB { return model.LOG_DB }, usageadapter.BatchLogs{}, billingmath.BatchMath{}, rediscache.Cache{})
}
func reserve(job *model.BatchJob, items []model.BatchItem) error {
	values := make([]domain.Item, len(items))
	for i := range items {
		values[i] = domain.Item(items[i])
	}
	return testStore().CreateBatchReservation((*domain.Job)(job), values)
}
