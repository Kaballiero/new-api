package model

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUsageAggregateDatabaseMatrix(t *testing.T) {
	runOnEveryQuotaDialect(t, func(t *testing.T) {
		var version string
		switch common.MainDatabaseType() {
		case common.DatabaseTypeSQLite:
			require.NoError(t, DB.Raw("SELECT sqlite_version()").Scan(&version).Error)
		case common.DatabaseTypeMySQL:
			require.NoError(t, DB.Raw("SELECT VERSION()").Scan(&version).Error)
		case common.DatabaseTypePostgreSQL:
			require.NoError(t, DB.Raw("SHOW server_version").Scan(&version).Error)
		}
		t.Logf("database version: %s", version)
		if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			require.NoError(t, DB.Exec("CREATE TABLE users (id BIGINT PRIMARY KEY)").Error)
			t.Cleanup(func() { DB.Exec("DROP TABLE users") })
		}
		for index := range MaxAggregateUsers {
			id := index + 1
			if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
				require.NoError(t, DB.Create(&User{Id: id, Username: fmt.Sprintf("aggregate-%d", id), AffCode: fmt.Sprintf("agg%d", id)}).Error)
			} else {
				require.NoError(t, DB.Table("users").Create(map[string]any{"id": id}).Error)
			}
		}
		start := unixUTC(2024, time.January, 31, 23, 0, 0)
		end := unixUTC(2024, time.February, 1, 0, 0, 0)
		for _, row := range []QuotaData{
			{UserID: 1, Username: "before-rename", ModelName: "Model", CreatedAt: start, Count: 2, Quota: 2147483648, TokenUsed: 11},
			{UserID: 1, Username: "after-rename", ModelName: "Model", CreatedAt: end, Count: 3, Quota: 7, TokenUsed: 13},
			{UserID: 2, Username: "two", ModelName: "model", CreatedAt: end, Count: 4, Quota: 9, TokenUsed: 17},
			{UserID: 2, Username: "two", ModelName: "Model ", CreatedAt: end, Count: 0, Quota: 0, TokenUsed: 0},
			{UserID: 3, Username: "outsider", ModelName: "Model", CreatedAt: end, Count: 99, Quota: 99999},
			{UserID: 1, ModelName: "outside-range", CreatedAt: start - 1, Count: 99, Quota: 99999},
			{UserID: 1, ModelName: "outside-range", CreatedAt: end + 1, Count: 99, Quota: 99999},
		} {
			seedBucketQuotaData(t, row)
		}
		scope := AggregateScope{UserIDs: []int{2, 1, 1}, StartTimestamp: &start, EndTimestamp: &end}
		for _, granularity := range []string{"hour", "day", "week", "month"} {
			rows, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "time", Granularity: granularity})
			require.NoError(t, err)
			if granularity == "week" {
				require.Len(t, rows, 1)
				assert.Equal(t, unixUTC(2024, time.January, 29, 0, 0, 0), *rows[0].CreatedAt)
				assert.Equal(t, int64(2147483664), rows[0].Quota)
				assert.Equal(t, int64(9), rows[0].Count)
			} else {
				require.Len(t, rows, 2)
				expectedStart := start
				if granularity == "day" {
					expectedStart = unixUTC(2024, time.January, 31, 0, 0, 0)
				}
				if granularity == "month" {
					expectedStart = unixUTC(2024, time.January, 1, 0, 0, 0)
				}
				assert.Equal(t, expectedStart, *rows[0].CreatedAt)
				assert.Less(t, *rows[0].CreatedAt, *rows[1].CreatedAt)
				assert.Equal(t, int64(2147483648), rows[0].Quota)
				assert.Equal(t, int64(16), rows[1].Quota)
			}
		}
		models, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "model", Granularity: "day"})
		require.NoError(t, err)
		require.Len(t, models, 3)
		assert.Equal(t, "Model", *models[0].ModelName)
		assert.Equal(t, int64(2147483655), models[0].Quota)
		assert.Equal(t, "Model ", *models[1].ModelName)
		assert.Zero(t, models[1].Quota)
		assert.Equal(t, "model", *models[2].ModelName)
		assert.Nil(t, models[0].UserID)
		assert.Nil(t, models[0].CreatedAt)
		users, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		require.NoError(t, err)
		require.Len(t, users, 2)
		assert.Equal(t, 1, *users[0].UserID)
		assert.Equal(t, int64(5), users[0].Count)
		assert.Equal(t, int64(24), users[0].TokenUsed)
		scope.UserIDs = []int{1, 2}
		permuted, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		require.NoError(t, err)
		assert.Equal(t, users, permuted)
		scope.UserIDs = make([]int, MaxAggregateUsers)
		for i := range scope.UserIDs {
			scope.UserIDs[i] = i + 1
		}
		_, err = GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "time_model", Granularity: "month"})
		require.NoError(t, err)
		scope.UserIDs = append(scope.UserIDs, 1)
		_, err = GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		assert.ErrorIs(t, err, ErrInvalidAggregate)
		scope.UserIDs = []int{999}
		_, err = GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		assert.ErrorIs(t, err, ErrAggregateUserNotFound)
		scope.UserIDs = []int{100}
		empty, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		require.NoError(t, err)
		assert.Empty(t, empty)

		scope.UserIDs = []int{1}
		require.NoError(t, DB.Table("quota_data").Where("user_id = ? AND created_at = ?", 1, start).Update("count", int64(2147483648)).Error)
		largeCount, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		require.NoError(t, err)
		require.Len(t, largeCount, 1)
		assert.Equal(t, int64(2147483651), largeCount[0].Count)
		monthStart := unixUTC(2020, time.January, 1, 0, 0, 0)
		monthEnd := unixUTC(2029, time.December, 31, 23, 59, 59)
		maximumIDs := make([]int, MaxAggregateUsers)
		for index := range maximumIDs {
			maximumIDs[index] = index + 1
		}
		maximum, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: AggregateScope{UserIDs: maximumIDs, StartTimestamp: &monthStart, EndTimestamp: &monthEnd}, GroupBy: "time_model", Granularity: "month"})
		require.NoError(t, err)
		require.Len(t, maximum, 6)
		var maximumQuota, maximumCount, maximumTokens int64
		for _, row := range maximum {
			maximumQuota += row.Quota
			maximumCount += row.Count
			maximumTokens += row.TokenUsed
		}
		assert.Equal(t, int64(2147783661), maximumQuota)
		assert.Equal(t, int64(2147483952), maximumCount)
		assert.Equal(t, int64(41), maximumTokens)
		monthEnd = unixUTC(2030, time.January, 1, 0, 0, 0)
		_, err = GetUsageAggregate(UsageAggregateQuery{AggregateScope: AggregateScope{UserIDs: []int{1}, StartTimestamp: &monthStart, EndTimestamp: &monthEnd}, GroupBy: "time", Granularity: "month"})
		assert.ErrorIs(t, err, ErrInvalidAggregate)
		require.NoError(t, DB.Table("quota_data").Where("user_id = ? AND created_at = ?", 1, start).Update("quota", MaxAggregateInteger).Error)
		_, err = GetUsageAggregate(UsageAggregateQuery{AggregateScope: scope, GroupBy: "user", Granularity: "day"})
		assert.ErrorIs(t, err, ErrAggregateOverflow)
	})
}

func TestLogAggregateUsesSeparateLogDatabase(t *testing.T) {
	truncateTables(t)
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := logDB.DB()
	require.NoError(t, err)
	previous := LOG_DB
	LOG_DB = logDB
	t.Cleanup(func() { LOG_DB = previous; sqlDB.Close() })
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	require.NoError(t, DB.Create(&User{Id: 1, Username: "selected", AffCode: "selected"}).Error)
	start, end := int64(100), int64(200)
	for _, row := range []Log{
		{UserId: 1, CreatedAt: start, Type: LogTypeConsume, Quota: 2147483648},
		{UserId: 1, CreatedAt: end, Type: LogTypeConsume, Quota: 7},
		{UserId: 1, CreatedAt: 150, Type: LogTypeConsume, Quota: 0},
		{UserId: 1, CreatedAt: end, Type: LogTypeRefund, Quota: 99},
		{UserId: 2, CreatedAt: end, Type: LogTypeConsume, Quota: 99999},
		{UserId: 1, CreatedAt: end + 1, Type: LogTypeConsume, Quota: 99999},
	} {
		require.NoError(t, logDB.Create(&row).Error)
	}
	result, err := GetLogAggregate(AggregateScope{UserIDs: []int{1, 1}, StartTimestamp: &start, EndTimestamp: &end})
	require.NoError(t, err)
	assert.Equal(t, LogAggregate{Quota: 2147483655, RefundQuota: 99, RequestCount: 3}, result)
	refundOnly, err := GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &end, EndTimestamp: &end})
	require.NoError(t, err)
	assert.Equal(t, LogAggregate{Quota: 7, RefundQuota: 99, RequestCount: 1}, refundOnly)
	zeroTime := int64(150)
	zero, err := GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &zeroTime, EndTimestamp: &zeroTime})
	require.NoError(t, err)
	assert.Equal(t, LogAggregate{Quota: 0, RequestCount: 1}, zero)
	emptyStart, emptyEnd := int64(300), int64(400)
	empty, err := GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &emptyStart, EndTimestamp: &emptyEnd})
	require.NoError(t, err)
	assert.Equal(t, LogAggregate{}, empty)
	_, err = GetLogAggregate(AggregateScope{StartTimestamp: &start, EndTimestamp: &end})
	assert.ErrorIs(t, err, ErrInvalidAggregate)

	// Stat keeps quota gross, reports refunds separately and counts rpm/tpm on consumption only.
	now := time.Now().Unix()
	for _, row := range []Log{
		{UserId: 1, CreatedAt: now, Type: LogTypeConsume, Quota: 5, PromptTokens: 3, CompletionTokens: 4},
		{UserId: 1, CreatedAt: now, Type: LogTypeRefund, Quota: 2, PromptTokens: 100, CompletionTokens: 100},
		{UserId: 1, CreatedAt: now, Type: LogTypeError, Quota: 1000, PromptTokens: 100},
	} {
		require.NoError(t, logDB.Create(&row).Error)
	}
	stat, err := SumUsedQuota(LogTypeUnknown, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, Stat{Quota: 2147683658, RefundQuota: 101, Rpm: 1, Tpm: 7}, stat)
	stat, err = SumUsedQuota(LogTypeUnknown, start, end, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, Stat{Quota: 2147583654, RefundQuota: 99, Rpm: 1, Tpm: 7}, stat)
	require.NoError(t, logDB.Create(&Log{UserId: 1, CreatedAt: start, Type: LogTypeRefund, Quota: int(MaxAggregateInteger)}).Error)
	_, err = GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &start, EndTimestamp: &end})
	assert.ErrorIs(t, err, ErrAggregateOverflow)
}

func TestLogAggregateDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres", "clickhouse"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_AGGREGATE_LOG_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("isolated log database is not configured")
			}
			truncateTables(t)
			t.Setenv("AGGREGATE_LOG_TEST_DSN", dsn)
			logDB, databaseType, err := chooseDB("AGGREGATE_LOG_TEST_DSN", true)
			require.NoError(t, err)
			require.Equal(t, "usage_aggregate_log_test", logDB.Migrator().CurrentDatabase(), "fixtures require a dedicated isolated log database")
			previousDB := LOG_DB
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			LOG_DB = logDB
			common.SetDatabaseTypes(previousMainType, databaseType)
			t.Cleanup(func() {
				LOG_DB.Exec("DROP TABLE logs")
				sqlDB, err := LOG_DB.DB()
				if err == nil {
					sqlDB.Close()
				}
				LOG_DB = previousDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
			})
			require.NoError(t, LOG_DB.Exec("DROP TABLE IF EXISTS logs").Error)
			ddl := "CREATE TABLE logs (user_id BIGINT, created_at BIGINT, type INTEGER, quota BIGINT, prompt_tokens INTEGER, completion_tokens INTEGER)"
			if databaseType == common.DatabaseTypeClickHouse {
				ddl = "CREATE TABLE logs (user_id Int64, created_at Int64, type Int32, quota Int64, prompt_tokens Int32, completion_tokens Int32) ENGINE=MergeTree ORDER BY tuple()"
			}
			require.NoError(t, LOG_DB.Exec(ddl).Error)
			require.NoError(t, DB.Create(&User{Id: 1, Username: "aggregate-log", AffCode: "agg-log"}).Error)
			for _, row := range []struct{ user, created, kind, quota int64 }{
				{1, 100, LogTypeConsume, 2147483648}, {1, 200, LogTypeConsume, 7}, {1, 150, LogTypeConsume, 0},
				{1, 200, LogTypeRefund, 99}, {2, 200, LogTypeConsume, 99999},
				{1, 201, LogTypeConsume, 99999}, {1, 99, LogTypeConsume, 99999},
			} {
				require.NoError(t, LOG_DB.Exec("INSERT INTO logs (user_id, created_at, type, quota) VALUES (?, ?, ?, ?)", row.user, row.created, row.kind, row.quota).Error)
			}
			start, end := int64(100), int64(200)
			scope := AggregateScope{UserIDs: []int{1, 1}, StartTimestamp: &start, EndTimestamp: &end}
			result, err := GetLogAggregate(scope)
			require.NoError(t, err)
			assert.Equal(t, LogAggregate{Quota: 2147483655, RefundQuota: 99, RequestCount: 3}, result)
			refundOnly, err := GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &end, EndTimestamp: &end})
			require.NoError(t, err)
			assert.Equal(t, LogAggregate{Quota: 7, RefundQuota: 99, RequestCount: 1}, refundOnly)
			stat, err := SumUsedQuota(LogTypeUnknown, start, end, "", "", "", 0, "")
			require.NoError(t, err)
			assert.Equal(t, Stat{Quota: 2147583654, RefundQuota: 99}, stat)
			zeroTime := int64(150)
			zero, err := GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &zeroTime, EndTimestamp: &zeroTime})
			require.NoError(t, err)
			assert.Equal(t, LogAggregate{Quota: 0, RequestCount: 1}, zero)
			emptyStart, emptyEnd := int64(300), int64(400)
			empty, err := GetLogAggregate(AggregateScope{UserIDs: []int{1}, StartTimestamp: &emptyStart, EndTimestamp: &emptyEnd})
			require.NoError(t, err)
			assert.Equal(t, LogAggregate{}, empty)
			require.NoError(t, LOG_DB.Exec("INSERT INTO logs (user_id, created_at, type, quota) VALUES (?, ?, ?, ?)", 1, start, LogTypeConsume, MaxAggregateInteger).Error)
			_, err = GetLogAggregate(scope)
			assert.ErrorIs(t, err, ErrAggregateOverflow)
			for range 2 {
				require.NoError(t, LOG_DB.Exec("INSERT INTO logs (user_id, created_at, type, quota) VALUES (?, ?, ?, ?)", 1, start, LogTypeConsume, int64(1<<63-1)).Error)
			}
			_, err = GetLogAggregate(scope)
			assert.Error(t, err)
		})
	}
}

func TestUsageAggregateRejectsOversizedProjection(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "row-limit", AffCode: "row-limit"}).Error)
	rows := make([]QuotaData, MaxAggregateRows+1)
	for index := range rows {
		rows[index] = QuotaData{UserID: 1, ModelName: fmt.Sprintf("model-%05d", index), CreatedAt: 100, Count: 1, Quota: 1}
	}
	require.NoError(t, DB.CreateInBatches(rows, 100).Error)
	start, end := int64(100), int64(100)
	_, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: AggregateScope{UserIDs: []int{1}, StartTimestamp: &start, EndTimestamp: &end}, GroupBy: "model", Granularity: "day"})
	assert.ErrorIs(t, err, ErrAggregateTooLarge)
}

func TestUsageAggregateRejectsInvalidRequests(t *testing.T) {
	start, end := int64(100), int64(200)
	negative, unsafe := int64(-1), MaxAggregateInteger+1
	for _, check := range []struct {
		name               string
		ids                []int
		start, end         *int64
		group, granularity string
	}{
		{"missing IDs", nil, &start, &end, "user", "day"},
		{"empty IDs", []int{}, &start, &end, "user", "day"},
		{"zero ID", []int{0}, &start, &end, "user", "day"},
		{"negative ID", []int{-1}, &start, &end, "user", "day"},
		{"missing start", []int{1}, nil, &end, "user", "day"},
		{"missing end", []int{1}, &start, nil, "user", "day"},
		{"negative start", []int{1}, &negative, &end, "user", "day"},
		{"reversed range", []int{1}, &end, &start, "user", "day"},
		{"unsafe range", []int{1}, &start, &unsafe, "user", "day"},
		{"missing grouping", []int{1}, &start, &end, "", "day"},
		{"SQL grouping", []int{1}, &start, &end, "model_name; DROP TABLE users", "day"},
		{"missing granularity", []int{1}, &start, &end, "model", ""},
		{"unsupported granularity", []int{1}, &start, &end, "model", "minute"},
	} {
		t.Run(check.name, func(t *testing.T) {
			_, err := GetUsageAggregate(UsageAggregateQuery{AggregateScope: AggregateScope{UserIDs: check.ids, StartTimestamp: check.start, EndTimestamp: check.end}, GroupBy: check.group, Granularity: check.granularity})
			assert.ErrorIs(t, err, ErrInvalidAggregate)
		})
	}
}
