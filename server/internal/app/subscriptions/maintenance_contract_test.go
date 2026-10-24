package subscriptions

import (
	"crypto/sha256"
	"database/sql/driver"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	sqliteclock "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const subscriptionTickOrigin = "6b0f439a432ab129817e4e41ce323e6bd3c3c0c3"

var subscriptionTickUpdate = flag.Bool("update", false, "record subscription tick goldens only on verified origin")
var subscriptionTickPreflight = flag.Bool("subscriptions-tick-preflight", false, "assert deterministic maintenance without goldens")
var subscriptionTickManifest = flag.Bool("subscriptions-tick-manifest", false, "print maintenance cases without fixture IO")
var subscriptionTickOriginCheck = flag.Bool("subscriptions-tick-origin-check", false, "validate recording boundary without writing maintenance goldens")

func subscriptionTickCases() []string {
	return []string{"mixed", "running", "cleanup_recent", "cleanup_boundary", "expire_error", "reset_error", "cleanup_error", "missing_plan", "advance_periods", "daily", "weekly", "monthly", "never"}
}

// TestSubscriptionMaintenanceContract calls the task body, never a ticker or listener.
func TestSubscriptionMaintenanceContract(t *testing.T) {
	if *subscriptionTickOriginCheck {
		require.False(t, *subscriptionTickUpdate)
		verifySubscriptionTickOrigin(t)
		return
	}
	if *subscriptionTickManifest {
		require.False(t, *subscriptionTickUpdate)
		data, err := common.Marshal(subscriptionTickCases())
		require.NoError(t, err)
		fmt.Println("SUBSCRIPTIONS_TICK_MANIFEST " + string(data))
		return
	}
	if *subscriptionTickUpdate {
		verifySubscriptionTickOrigin(t)
	}
	name := os.Getenv("SUBSCRIPTIONS_TICK_CASE")
	if name == "" {
		for _, name := range subscriptionTickCases() {
			t.Run(name, func(t *testing.T) {
				exe, err := os.Executable()
				require.NoError(t, err)
				cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestSubscriptionMaintenanceContract$", "-test.count=1", "-test.timeout=60s",
					"-update="+strconv.FormatBool(*subscriptionTickUpdate), "-subscriptions-tick-preflight="+strconv.FormatBool(*subscriptionTickPreflight))
				cmd.Env = append(os.Environ(), "SUBSCRIPTIONS_TICK_CASE="+name, "TZ=UTC")
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, "SUBSCRIPTIONS_TICK_SNAPSHOT ") {
						fmt.Println(line)
					}
				}
			})
		}
		return
	}
	const epoch int64 = 946684800 // The synctest clock begins at 2000-01-01 UTC.
	clockCalls := 0
	require.NoError(t, sqliteclock.RegisterScalarFunction("strftime", 2, func(_ *sqliteclock.FunctionContext, args []driver.Value) (driver.Value, error) {
		if len(args) != 2 || args[0] != "%s" || args[1] != "now" {
			return nil, fmt.Errorf("unexpected fixture clock arguments: %v", args)
		}
		clockCalls++
		return strconv.FormatInt(epoch, 10), nil
	}))
	// Initialize janitors outside the synctest bubble; the production cache is process-local.
	model.InvalidateSubscriptionPlanCache(71)
	// InitDB initializes dialect-dependent column names that direct gorm.Open does not set.
	previousDB, previousPath := model.DB, common.SQLitePath
	t.Setenv("SQL_DSN", "")
	common.SQLitePath = filepath.Join(t.TempDir(), "dialect.db")
	require.NoError(t, model.InitDB())
	dialectPool, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, dialectPool.Close())
	model.DB, common.SQLitePath = previousDB, previousPath
	synctest.Test(t, func(t *testing.T) {
		require.Equal(t, epoch, time.Now().Unix())
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "maintenance.db")), &gorm.Config{})
		require.NoError(t, err)
		pool, err := db.DB()
		require.NoError(t, err)
		defer pool.Close()
		previousDB, previousLog := model.DB, model.LOG_DB
		previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
		previousRedis, previousDebug := common.RedisEnabled, common.DebugEnabled
		defer func() {
			model.DB, model.LOG_DB = previousDB, previousLog
			common.SetDatabaseTypes(previousMainType, previousLogType)
			common.RedisEnabled, common.DebugEnabled = previousRedis, previousDebug
			subscriptionResetRunning.Store(false)
			subscriptionCleanupLast.Store(0)
		}()
		model.DB, model.LOG_DB = db, db
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		common.RedisEnabled, common.DebugEnabled = false, false
		require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
		// Assert the actual DB expression independently: a wall-clock fallback cannot satisfy this probe.
		var actual int64
		require.NoError(t, db.Raw("SELECT strftime('%s','now')").Scan(&actual).Error)
		require.Equal(t, epoch, actual)
		require.Equal(t, epoch, model.GetDBTimestamp())
		require.GreaterOrEqual(t, clockCalls, 2)
		require.NoError(t, db.Create(&model.User{Id: 1, Username: "maintenance-user", Group: "pro", AuthVersion: 3}).Error)
		plan := model.SubscriptionPlan{Id: 71, Title: "Maintenance plan", DurationUnit: "month", DurationValue: 1, Enabled: true, TotalAmount: 100,
			QuotaResetPeriod: "custom", QuotaResetCustomSeconds: 3600}
		require.NoError(t, db.Create(&plan).Error)
		subs := []model.UserSubscription{
			{Id: 11, UserId: 1, PlanId: 71, AmountTotal: 100, AmountUsed: 40, StartTime: epoch - 7200, EndTime: epoch + 86400, Status: "active", LastResetTime: epoch - 7200, NextResetTime: epoch - 3600},
			{Id: 12, UserId: 1, PlanId: 71, AmountTotal: 100, AmountUsed: 50, StartTime: epoch - 7200, EndTime: epoch + 86400, Status: "active", LastResetTime: epoch, NextResetTime: epoch + 3600},
			{Id: 13, UserId: 1, PlanId: 71, AmountTotal: 100, AmountUsed: 60, StartTime: epoch - 7200, EndTime: epoch, Status: "active", LastResetTime: epoch - 7200, NextResetTime: epoch - 3600, UpgradeGroup: "pro", PrevUserGroup: "default"},
		}
		require.NoError(t, db.Create(&subs).Error)
		for _, row := range []model.SubscriptionPreConsumeRecord{
			{Id: 21, RequestId: "old-consumed", UserId: 1, PreConsumed: 1, Status: "consumed"},
			{Id: 22, RequestId: "old-refunded", UserId: 1, PreConsumed: 1, Status: "refunded"},
			{Id: 23, RequestId: "boundary", UserId: 1, PreConsumed: 1, Status: "consumed"},
			{Id: 24, RequestId: "recent", UserId: 1, PreConsumed: 1, Status: "consumed"},
		} {
			require.NoError(t, db.Create(&row).Error)
		}
		require.NoError(t, db.Model(&model.SubscriptionPreConsumeRecord{}).Where("id IN ?", []int{21, 22}).UpdateColumn("updated_at", epoch-604801).Error)
		require.NoError(t, db.Model(&model.SubscriptionPreConsumeRecord{}).Where("id = ?", 23).UpdateColumn("updated_at", epoch-604800).Error)
		subscriptionResetRunning.Store(name == "running")
		subscriptionCleanupLast.Store(0)
		if name == "cleanup_recent" {
			subscriptionCleanupLast.Store(epoch - 1799)
		}
		if name == "cleanup_boundary" {
			subscriptionCleanupLast.Store(epoch - 1800)
		}
		if name == "daily" || name == "weekly" || name == "monthly" || name == "never" {
			require.NoError(t, db.Model(&model.SubscriptionPlan{}).Where("id = ?", 71).Update("quota_reset_period", name).Error)
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 11).UpdateColumn("end_time", epoch+90*86400).Error)
			if name == "weekly" {
				require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 11).UpdateColumns(map[string]any{"last_reset_time": epoch - 7*86400, "next_reset_time": epoch - 5*86400}).Error)
			}
		}
		if name == "missing_plan" {
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 11).UpdateColumn("plan_id", 999).Error)
		}
		if name == "advance_periods" {
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("id = ?", 11).UpdateColumns(map[string]any{"last_reset_time": epoch - 18000, "next_reset_time": epoch - 14400}).Error)
		}
		queryCount := 0
		if name == "expire_error" || name == "reset_error" {
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("subscription_tick_query_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "user_subscriptions" {
					queryCount++
					if name == "expire_error" || (name == "reset_error" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"]), "next_reset_time")) {
						tx.AddError(fmt.Errorf("fixture subscription query failure"))
					}
				}
			}))
		}
		if name == "cleanup_error" {
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("subscription_tick_cleanup_failure", func(tx *gorm.DB) {
				tx.AddError(fmt.Errorf("fixture cleanup failure"))
			}))
		}
		runSubscriptionQuotaResetOnce()
		if name == "expire_error" || name == "reset_error" {
			require.Positive(t, queryCount)
			require.NoError(t, db.Callback().Query().Remove("subscription_tick_query_failure"))
		}
		var rows []model.UserSubscription
		require.NoError(t, db.Order("id").Find(&rows).Error)
		require.Len(t, rows, 3)
		reset := name != "running" && name != "expire_error" && name != "reset_error" && name != "missing_plan" && name != "never"
		if reset {
			assert.EqualValues(t, 0, rows[0].AmountUsed)
			last, next := epoch, epoch+3600
			switch name {
			case "daily":
				next = epoch + 86400
			case "weekly":
				last, next = epoch-5*86400, epoch+2*86400
			case "monthly":
				next = epoch + 31*86400
			}
			assert.Equal(t, last, rows[0].LastResetTime)
			assert.Equal(t, next, rows[0].NextResetTime)
		} else {
			assert.EqualValues(t, 40, rows[0].AmountUsed)
		}
		assert.EqualValues(t, 50, rows[1].AmountUsed)
		wantStatus := "expired"
		if name == "running" || name == "expire_error" {
			wantStatus = "active"
		}
		assert.Equal(t, wantStatus, rows[2].Status)
		assert.EqualValues(t, 60, rows[2].AmountUsed, "expiry precedes reset")
		ledger := []model.SubscriptionPreConsumeRecord{}
		require.NoError(t, db.Order("id").Find(&ledger).Error)
		cleaned := name != "running" && name != "expire_error" && name != "reset_error" && name != "cleanup_recent" && name != "cleanup_error"
		if cleaned {
			require.Len(t, ledger, 2)
			assert.Equal(t, 23, ledger[0].Id, "cleanup is strictly older-than")
			assert.Equal(t, epoch, subscriptionCleanupLast.Load())
		} else {
			require.Len(t, ledger, 4)
			assert.NotEqual(t, epoch, subscriptionCleanupLast.Load())
		}
		var user struct {
			Group       string
			AuthVersion int64
		}
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Select("group", "auth_version").First(&user).Error)
		wantGroup := "default"
		if wantStatus == "active" {
			wantGroup = "pro"
		}
		assert.Equal(t, wantGroup, user.Group)
		assert.EqualValues(t, 3, user.AuthVersion)
		data, err := common.Marshal(struct {
			Subscriptions []model.UserSubscription             `json:"subscriptions"`
			Ledger        []model.SubscriptionPreConsumeRecord `json:"ledger"`
			User          any                                  `json:"user"`
			CleanupLast   int64                                `json:"cleanup_last"`
		}{rows, ledger, user, subscriptionCleanupLast.Load()})
		require.NoError(t, err)
		if *subscriptionTickPreflight {
			fmt.Printf("SUBSCRIPTIONS_TICK_SNAPSHOT %s %s\n", name, data)
			return
		}
		path := filepath.Join("..", "..", "..", "service", "testdata", "subscription-maintenance", name+".golden.json")
		if *subscriptionTickUpdate {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o644))
		}
		expected, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, string(expected), string(append(data, '\n')))
	})
}

func verifySubscriptionTickOrigin(t *testing.T) {
	t.Helper()
	require.False(t, *subscriptionTickPreflight || *subscriptionTickManifest)
	require.Equal(t, subscriptionTickOrigin, os.Getenv("SUBSCRIPTIONS_CONTRACT_ORIGIN_RECORD"))
	root, err := filepath.EvalSymlinks(os.Getenv("SUBSCRIPTIONS_CONTRACT_ORIGIN_DIR"))
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "service"), cwd)
	marker, err := os.ReadFile(filepath.Join(root, ".subscriptions-origin"))
	require.NoError(t, err)
	require.Equal(t, subscriptionTickOrigin+"\n", string(marker))
	data, err := os.ReadFile(filepath.Join(root, ".subscriptions-source.sha256"))
	require.NoError(t, err)
	var hashes map[string]string
	require.NoError(t, common.Unmarshal(data, &hashes))
	require.NotEmpty(t, hashes)
	for name, digest := range hashes {
		require.False(t, filepath.IsAbs(name) || strings.Contains(name, ".."))
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		require.Equal(t, digest, fmt.Sprintf("%x", sha256.Sum256(data)), name)
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		marker := filepath.Join(dir, ".git")
		info, err := os.Stat(marker)
		if !os.IsNotExist(err) {
			require.NoError(t, err)
			// Managed writable roots may contain an empty read-only sandbox placeholder.
			require.Contains(t, []string{"/tmp", "/dev/shm"}, dir)
			require.True(t, info.IsDir())
			require.Zero(t, info.Mode().Perm()&0o222)
			entries, err := os.ReadDir(marker)
			require.NoError(t, err)
			require.Empty(t, entries)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
}
